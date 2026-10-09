package e2ee

import (
	"bytes"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const InitialOneTimePrekeys = 20

// RFC 9106 section 4's second recommended profile, with a 128-bit salt and
// 256-bit output. Keeping one exact profile prevents parameter downgrades.
func NewKDFProfile() (KDFProfile, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return KDFProfile{}, ErrCrypto
	}
	return KDFProfile{Version: 1, Algorithm: "argon2id", Salt: base64.StdEncoding.EncodeToString(salt),
		MemoryKiB: 65536, Iterations: 3, Parallelism: 4}, nil
}

func ValidateKDFProfile(profile KDFProfile) error {
	if profile.Version != 1 || profile.Algorithm != "argon2id" || profile.MemoryKiB != 65536 ||
		profile.Iterations != 3 || profile.Parallelism != 4 {
		return ErrInvalidKDF
	}
	if _, err := decode64(profile.Salt, 16); err != nil {
		return ErrInvalidKDF
	}
	return nil
}

func NormalizeAccountUsername(username string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !utf8.ValidString(username) || utf8.RuneCountInString(username) < 1 ||
		utf8.RuneCountInString(username) > 50 || strings.ContainsRune(username, 0) {
		return "", ErrInvalidContext
	}
	return username, nil
}

func DeriveCredentials(username, password string, profile KDFProfile) (PasswordKeys, error) {
	username, err := NormalizeAccountUsername(username)
	if err != nil {
		return PasswordKeys{}, err
	}
	if err := ValidateKDFProfile(profile); err != nil {
		return PasswordKeys{}, err
	}
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < 8 || len(password) > 1024 || strings.ContainsRune(password, 0) {
		return PasswordKeys{}, ErrInvalidPlaintext
	}
	salt, _ := decode64(profile.Salt, 16)
	passwordBytes := []byte(password)
	defer clear(passwordBytes)
	master := argon2.IDKey(passwordBytes, salt, profile.Iterations, profile.MemoryKiB, profile.Parallelism, 32)
	defer clear(master)
	auth, err := hkdf.Key(sha256.New, master, nil, "Mini-Hermes/auth-credential/v2\x00"+username, 32)
	if err != nil {
		return PasswordKeys{}, ErrCrypto
	}
	defer clear(auth)
	vault, err := hkdf.Key(sha256.New, master, nil, "Mini-Hermes/account-vault-key/v2\x00"+username, 32)
	if err != nil {
		return PasswordKeys{}, ErrCrypto
	}
	defer clear(vault)
	return PasswordKeys{AuthCredential: base64.StdEncoding.EncodeToString(auth), VaultKey: [32]byte(vault)}, nil
}

func EncodePrivateKeyPair(pair KeyPair) PrivateKeyPair {
	return PrivateKeyPair{PrivateKey: base64.StdEncoding.EncodeToString(pair.Private[:]),
		PublicKey: base64.StdEncoding.EncodeToString(pair.Public[:])}
}

func DecodePrivateKeyPair(value PrivateKeyPair) (KeyPair, error) {
	private, err := decode64(value.PrivateKey, 32)
	if err != nil {
		return KeyPair{}, ErrInvalidKey
	}
	defer clear(private)
	public, err := publicKey(value.PublicKey)
	if err != nil {
		return KeyPair{}, ErrInvalidKey
	}
	pair := KeyPair{Private: [32]byte(private), Public: public}
	if err := validatePair(pair); err != nil {
		return KeyPair{}, err
	}
	return pair, nil
}

func GenerateAccountVault() (AccountVault, UploadRequest, error) {
	identity, err := GenerateKeyPair()
	if err != nil {
		return AccountVault{}, UploadRequest{}, err
	}
	signed, err := GenerateKeyPair()
	if err != nil {
		return AccountVault{}, UploadRequest{}, err
	}
	signature, err := SignPrekey(identity.Private, signed.Public)
	if err != nil {
		return AccountVault{}, UploadRequest{}, err
	}
	vault := AccountVault{Version: 1, Identity: EncodePrivateKeyPair(identity),
		SignedPrekey: VaultSignedPrekey{KeyID: 1, KeyPair: EncodePrivateKeyPair(signed),
			Signature: base64.StdEncoding.EncodeToString(signature[:])},
		OneTimePrekeys: make([]VaultOneTimePrekey, 0, InitialOneTimePrekeys)}
	for index := range InitialOneTimePrekeys {
		pair, err := GenerateKeyPair()
		if err != nil {
			return AccountVault{}, UploadRequest{}, err
		}
		vault.OneTimePrekeys = append(vault.OneTimePrekeys, VaultOneTimePrekey{KeyID: int64(index + 2), KeyPair: EncodePrivateKeyPair(pair)})
	}
	public := PublicBundleFromVault(vault)
	return vault, public, nil
}

func PublicBundleFromVault(vault AccountVault) UploadRequest {
	public := UploadRequest{IdentityPublicKey: vault.Identity.PublicKey,
		SignedPrekey:   SignedPrekey{KeyID: vault.SignedPrekey.KeyID, PublicKey: vault.SignedPrekey.KeyPair.PublicKey, Signature: vault.SignedPrekey.Signature},
		OneTimePrekeys: make([]PublicPrekey, 0, len(vault.OneTimePrekeys))}
	for _, opk := range vault.OneTimePrekeys {
		public.OneTimePrekeys = append(public.OneTimePrekeys, PublicPrekey{KeyID: opk.KeyID, PublicKey: opk.KeyPair.PublicKey})
	}
	return canonicalPublicBundle(public)
}

func canonicalPublicBundle(public UploadRequest) UploadRequest {
	public.OneTimePrekeys = append([]PublicPrekey(nil), public.OneTimePrekeys...)
	sort.Slice(public.OneTimePrekeys, func(i, j int) bool { return public.OneTimePrekeys[i].KeyID < public.OneTimePrekeys[j].KeyID })
	return public
}

// Registration is independent of the UUID the server will assign afterwards.
func ValidatePublicBundle(public UploadRequest) error {
	if public.SignedPrekey.KeyID != 1 || len(public.OneTimePrekeys) != InitialOneTimePrekeys ||
		verifyPrekeyBundle(public.IdentityPublicKey, public.SignedPrekey, nil) != nil {
		return ErrInvalidBundle
	}
	seen := make(map[int64]bool, InitialOneTimePrekeys)
	for _, opk := range public.OneTimePrekeys {
		if opk.KeyID < 2 || opk.KeyID > InitialOneTimePrekeys+1 || seen[opk.KeyID] {
			return ErrInvalidBundle
		}
		if _, err := publicKey(opk.PublicKey); err != nil {
			return ErrInvalidBundle
		}
		seen[opk.KeyID] = true
	}
	return nil
}

func ValidateAccountVault(vault AccountVault, public UploadRequest) error {
	if vault.Version != 1 || ValidatePublicBundle(public) != nil || len(vault.OneTimePrekeys) != InitialOneTimePrekeys {
		return ErrInvalidVault
	}
	if _, err := DecodePrivateKeyPair(vault.Identity); err != nil {
		return ErrInvalidVault
	}
	if _, err := DecodePrivateKeyPair(vault.SignedPrekey.KeyPair); err != nil {
		return ErrInvalidVault
	}
	for _, opk := range vault.OneTimePrekeys {
		if _, err := DecodePrivateKeyPair(opk.KeyPair); err != nil {
			return ErrInvalidVault
		}
	}
	actual, _ := json.Marshal(PublicBundleFromVault(vault))
	expected, _ := json.Marshal(canonicalPublicBundle(public))
	if !bytes.Equal(actual, expected) {
		return ErrInvalidVault
	}
	return nil
}

func vaultAAD(username string, profile KDFProfile, public UploadRequest) ([]byte, error) {
	username, err := NormalizeAccountUsername(username)
	if err != nil {
		return nil, err
	}
	if ValidateKDFProfile(profile) != nil || ValidatePublicBundle(public) != nil {
		return nil, ErrInvalidVault
	}
	context, err := json.Marshal(struct {
		Username     string        `json:"username"`
		KDF          KDFProfile    `json:"kdf"`
		PublicBundle UploadRequest `json:"public_bundle"`
	}{username, profile, canonicalPublicBundle(public)})
	if err != nil {
		return nil, ErrCrypto
	}
	return append([]byte("Mini-Hermes/account-vault/v2\x00"), context...), nil
}

func EncryptAccountVault(key [32]byte, username string, profile KDFProfile, public UploadRequest, vault AccountVault) (EncryptedRecord, error) {
	if err := ValidateAccountVault(vault, public); err != nil {
		return EncryptedRecord{}, err
	}
	aad, err := vaultAAD(username, profile, public)
	if err != nil {
		return EncryptedRecord{}, err
	}
	plaintext, err := json.Marshal(vault)
	if err != nil {
		return EncryptedRecord{}, ErrCrypto
	}
	defer clear(plaintext)
	return sealRecord(key, plaintext, aad)
}

func OpenAccountVault(key [32]byte, username string, profile KDFProfile, public UploadRequest, record EncryptedRecord) (AccountVault, error) {
	aad, err := vaultAAD(username, profile, public)
	if err != nil {
		return AccountVault{}, err
	}
	plaintext, err := openRecord(key, record, aad, MaxVaultCiphertextBytes)
	if err != nil {
		return AccountVault{}, err
	}
	defer clear(plaintext)
	var vault AccountVault
	if err := decodeExact(plaintext, []string{"version", "identity", "signed_prekey", "one_time_prekeys"}, nil, &vault); err != nil {
		return AccountVault{}, ErrInvalidVault
	}
	if err := validateVaultWire(plaintext); err != nil {
		return AccountVault{}, ErrInvalidVault
	}
	if err := ValidateAccountVault(vault, public); err != nil {
		return AccountVault{}, err
	}
	return vault, nil
}

func validateVaultWire(plaintext []byte) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(plaintext, &fields) != nil {
		return ErrInvalidVault
	}
	var pair PrivateKeyPair
	if decodeExact(fields["identity"], []string{"private_key", "public_key"}, nil, &pair) != nil {
		return ErrInvalidVault
	}
	var signed map[string]json.RawMessage
	if decodeExact(fields["signed_prekey"], []string{"key_id", "key_pair", "signature"}, nil, &signed) != nil ||
		decodeExact(signed["key_pair"], []string{"private_key", "public_key"}, nil, &pair) != nil {
		return ErrInvalidVault
	}
	var prekeys []json.RawMessage
	if json.Unmarshal(fields["one_time_prekeys"], &prekeys) != nil || len(prekeys) != InitialOneTimePrekeys {
		return ErrInvalidVault
	}
	for _, raw := range prekeys {
		var prekey map[string]json.RawMessage
		if decodeExact(raw, []string{"key_id", "key_pair"}, nil, &prekey) != nil ||
			decodeExact(prekey["key_pair"], []string{"private_key", "public_key"}, nil, &pair) != nil {
			return ErrInvalidVault
		}
	}
	return nil
}
