package e2ee

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.mau.fi/libsignal/ecc"
)

// ecc's default logger is lazily initialized without synchronization.
// Serialize its calls without configuring or enabling library logging.
var signatureMu sync.Mutex

var (
	ErrInvalidKey       = errors.New("invalid key")
	ErrInvalidBundle    = errors.New("invalid bundle")
	ErrInvalidContext   = errors.New("invalid message context")
	ErrInvalidEnvelope  = errors.New("invalid envelope")
	ErrInvalidPlaintext = errors.New("invalid plaintext")
	ErrDecrypt          = errors.New("decryption failed")
	ErrCrypto           = errors.New("crypto failed")
	ErrInvalidKDF       = errors.New("invalid password KDF profile")
	ErrInvalidVault     = errors.New("invalid encrypted vault or backup")
)

func GenerateKeyPair() (KeyPair, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, ErrCrypto
	}
	var pair KeyPair
	copy(pair.Private[:], private.Bytes())
	copy(pair.Public[:], private.PublicKey().Bytes())
	return pair, nil
}

// An ECDH probe rejects every low-order point, including non-canonical aliases.
// The probe scalar is public, fixed and unrelated to any user's keys.
func ValidatePublicKey(public [32]byte) error {
	probe, _ := ecdh.X25519().NewPrivateKey(make([]byte, 32))
	peer, err := ecdh.X25519().NewPublicKey(public[:])
	if err != nil {
		return ErrInvalidKey
	}
	if _, err = probe.ECDH(peer); err != nil {
		return ErrInvalidKey
	}
	return nil
}

// SignPrekey uses libsignal's sign-bit signature variant, NOT canonical
// XEdDSA-2016 wire encoding. Only the type-prefixed public key is signed.
func SignPrekey(identityPrivate, signedPublic [32]byte) ([64]byte, error) {
	if err := ValidatePublicKey(signedPublic); err != nil {
		return [64]byte{}, err
	}
	signatureMu.Lock()
	defer signatureMu.Unlock()
	return ecc.CalculateSignature(ecc.NewDjbECPrivateKey(identityPrivate), encodePublic(signedPublic)), nil
}

func VerifyBundle(bundle Bundle) error {
	if _, err := canonicalUUID(bundle.UserID); err != nil {
		return ErrInvalidBundle
	}
	return verifyPrekeyBundle(bundle.IdentityPublicKey, bundle.SignedPrekey, bundle.OneTimePrekey)
}

func verifyPrekeyBundle(identityPublic string, signedPrekey SignedPrekey, oneTime *PublicPrekey) error {
	if signedPrekey.KeyID <= 0 {
		return ErrInvalidBundle
	}
	identity, err := publicKey(identityPublic)
	if err != nil {
		return ErrInvalidBundle
	}
	signed, err := publicKey(signedPrekey.PublicKey)
	if err != nil {
		return ErrInvalidBundle
	}
	signature, err := decode64(signedPrekey.Signature, 64)
	if err != nil {
		return ErrInvalidBundle
	}
	if !verifySignature(identity, signed, [64]byte(signature)) {
		return ErrInvalidBundle
	}
	if opk := oneTime; opk != nil {
		if opk.KeyID <= 0 || opk.KeyID == signedPrekey.KeyID {
			return ErrInvalidBundle
		}
		if _, err := publicKey(opk.PublicKey); err != nil {
			return ErrInvalidBundle
		}
	}
	return nil
}

func verifySignature(identity, signed [32]byte, signature [64]byte) bool {
	signatureMu.Lock()
	defer signatureMu.Unlock()
	return ecc.VerifySignature(ecc.NewDjbECPublicKey(identity), encodePublic(signed), signature)
}

func ParseEnvelope(content string) (Envelope, error) {
	if len(content) > 8192 || !utf8.ValidString(content) {
		return Envelope{}, ErrInvalidEnvelope
	}
	// Tokenize the top-level object to reject missing, duplicate, differently
	// cased or unknown fields; DisallowUnknownFields alone is case-insensitive.
	fields := map[string]bool{
		"version": false, "recipient_id": false, "sender_identity_key": false,
		"ephemeral_key": false, "signed_prekey_id": false, "one_time_prekey_id": false,
		"nonce": false, "ciphertext": false,
	}
	d := json.NewDecoder(bytes.NewBufferString(content))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return Envelope{}, ErrInvalidEnvelope
	}
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		seen, known := fields[name]
		if err != nil || !ok || !known || seen {
			return Envelope{}, ErrInvalidEnvelope
		}
		fields[name] = true
		var raw json.RawMessage
		if err := d.Decode(&raw); err != nil || (name != "one_time_prekey_id" && bytes.Equal(raw, []byte("null"))) {
			return Envelope{}, ErrInvalidEnvelope
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return Envelope{}, ErrInvalidEnvelope
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return Envelope{}, ErrInvalidEnvelope
	}
	for _, seen := range fields {
		if !seen {
			return Envelope{}, ErrInvalidEnvelope
		}
	}
	var envelope Envelope
	if json.Unmarshal([]byte(content), &envelope) != nil || validateEnvelope(envelope) != nil {
		return Envelope{}, ErrInvalidEnvelope
	}
	return envelope, nil
}

func EncodeEnvelope(envelope Envelope) (string, error) {
	if err := validateEnvelope(envelope); err != nil {
		return "", err
	}
	content, err := json.Marshal(envelope)
	if err != nil || len(content) > 8192 {
		return "", ErrInvalidEnvelope
	}
	return string(content), nil
}

// Seal retains the v1 profile for regression tests. Application messages use
// CreateEpoch once, then SealMessage; the WASM bridge does not expose v1.
func Seal(ctx MessageContext, identity KeyPair, recipient Bundle, plaintext []byte) (Envelope, [32]byte, error) {
	if err := validateContext(ctx); err != nil || ctx.RecipientID != recipient.UserID {
		return Envelope{}, [32]byte{}, ErrInvalidContext
	}
	if err := validatePair(identity); err != nil {
		return Envelope{}, [32]byte{}, err
	}
	if err := validatePlaintext(plaintext); err != nil {
		return Envelope{}, [32]byte{}, err
	}
	if err := VerifyBundle(recipient); err != nil {
		return Envelope{}, [32]byte{}, err
	}
	ephemeral, err := GenerateKeyPair()
	if err != nil {
		return Envelope{}, [32]byte{}, err
	}
	recipientIK, _ := publicKey(recipient.IdentityPublicKey)
	spk, _ := publicKey(recipient.SignedPrekey.PublicKey)
	inputs := [][2][32]byte{{identity.Private, spk}, {ephemeral.Private, recipientIK}, {ephemeral.Private, spk}}
	envelope := Envelope{
		Version: 1, RecipientID: ctx.RecipientID,
		SenderIdentityKey: base64.StdEncoding.EncodeToString(identity.Public[:]),
		EphemeralKey:      base64.StdEncoding.EncodeToString(ephemeral.Public[:]),
		SignedPrekeyID:    recipient.SignedPrekey.KeyID,
	}
	if opk := recipient.OneTimePrekey; opk != nil {
		public, _ := publicKey(opk.PublicKey)
		inputs = append(inputs, [2][32]byte{ephemeral.Private, public})
		id := opk.KeyID
		envelope.OneTimePrekeyID = &id
	}
	key, err := deriveKey(inputs)
	if err != nil {
		return Envelope{}, [32]byte{}, err
	}
	aad, err := makeAAD(ctx, identity.Public, recipientIK, envelope)
	if err != nil {
		return Envelope{}, [32]byte{}, err
	}
	aead, err := newGCM(key)
	if err != nil {
		return Envelope{}, [32]byte{}, err
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return Envelope{}, [32]byte{}, ErrCrypto
	}
	envelope.Nonce = base64.StdEncoding.EncodeToString(nonce)
	envelope.Ciphertext = base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, aad))
	return envelope, key, nil
}

func Open(ctx MessageContext, identity, signed KeyPair, oneTime *KeyPair, envelope Envelope) ([]byte, [32]byte, error) {
	if err := validateContext(ctx); err != nil || ctx.RecipientID != envelope.RecipientID {
		return nil, [32]byte{}, ErrInvalidContext
	}
	if err := validateEnvelope(envelope); err != nil {
		return nil, [32]byte{}, err
	}
	if validatePair(identity) != nil || validatePair(signed) != nil || (oneTime != nil && validatePair(*oneTime) != nil) {
		return nil, [32]byte{}, ErrInvalidKey
	}
	if (envelope.OneTimePrekeyID == nil) != (oneTime == nil) {
		return nil, [32]byte{}, ErrInvalidKey
	}
	senderIK, _ := publicKey(envelope.SenderIdentityKey)
	ephemeral, _ := publicKey(envelope.EphemeralKey)
	inputs := [][2][32]byte{{signed.Private, senderIK}, {identity.Private, ephemeral}, {signed.Private, ephemeral}}
	if oneTime != nil {
		inputs = append(inputs, [2][32]byte{oneTime.Private, ephemeral})
	}
	key, err := deriveKey(inputs)
	if err != nil {
		return nil, [32]byte{}, err
	}
	plaintext, err := DecryptWithKey(ctx, senderIK, identity.Public, key, envelope)
	if err != nil {
		return nil, [32]byte{}, err
	}
	return plaintext, key, nil
}

func DecryptWithKey(ctx MessageContext, senderIK, recipientIK, key [32]byte, envelope Envelope) ([]byte, error) {
	if err := validateContext(ctx); err != nil || ctx.RecipientID != envelope.RecipientID {
		return nil, ErrInvalidContext
	}
	if err := validateEnvelope(envelope); err != nil {
		return nil, err
	}
	actual, _ := publicKey(envelope.SenderIdentityKey)
	if actual != senderIK || ValidatePublicKey(senderIK) != nil || ValidatePublicKey(recipientIK) != nil {
		return nil, ErrInvalidKey
	}
	aad, err := makeAAD(ctx, senderIK, recipientIK, envelope)
	if err != nil {
		return nil, err
	}
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, _ := decode64(envelope.Nonce, 12)
	ciphertext, _ := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	if err := validatePlaintext(plaintext); err != nil {
		clear(plaintext)
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

func decode64(encoded string, length int) ([]byte, error) {
	if len(encoded) != base64.StdEncoding.EncodedLen(length) {
		return nil, ErrInvalidKey
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != length || base64.StdEncoding.EncodeToString(decoded) != encoded {
		return nil, ErrInvalidKey
	}
	return decoded, nil
}

func publicKey(encoded string) ([32]byte, error) {
	decoded, err := decode64(encoded, 32)
	if err != nil {
		return [32]byte{}, err
	}
	public := [32]byte(decoded)
	return public, ValidatePublicKey(public)
}

func encodePublic(public [32]byte) []byte {
	return append([]byte{0x05}, public[:]...)
}

func validatePair(pair KeyPair) error {
	private, err := ecdh.X25519().NewPrivateKey(pair.Private[:])
	if err != nil || !bytes.Equal(private.PublicKey().Bytes(), pair.Public[:]) {
		return ErrInvalidKey
	}
	return ValidatePublicKey(pair.Public)
}

func canonicalUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, ErrInvalidContext
	}
	return id, nil
}

func validateContext(ctx MessageContext) error {
	for _, value := range []string{ctx.ThreadID, ctx.MessageID, ctx.SenderID, ctx.RecipientID} {
		if _, err := canonicalUUID(value); err != nil {
			return err
		}
	}
	return nil
}

func validatePlaintext(plaintext []byte) error {
	count := utf8.RuneCount(plaintext)
	if !utf8.Valid(plaintext) || count < 1 || count > 1000 || bytes.IndexByte(plaintext, 0) >= 0 {
		return ErrInvalidPlaintext
	}
	return nil
}

func validateEnvelope(envelope Envelope) error {
	if envelope.Version != 1 || envelope.SignedPrekeyID <= 0 {
		return ErrInvalidEnvelope
	}
	if _, err := canonicalUUID(envelope.RecipientID); err != nil {
		return ErrInvalidEnvelope
	}
	if id := envelope.OneTimePrekeyID; id != nil && (*id <= 0 || *id == envelope.SignedPrekeyID) {
		return ErrInvalidEnvelope
	}
	if _, err := publicKey(envelope.SenderIdentityKey); err != nil {
		return ErrInvalidEnvelope
	}
	if _, err := publicKey(envelope.EphemeralKey); err != nil {
		return ErrInvalidEnvelope
	}
	if _, err := decode64(envelope.Nonce, 12); err != nil {
		return ErrInvalidEnvelope
	}
	ciphertext, err := base64.StdEncoding.Strict().DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) < 17 || len(ciphertext) > 4016 || base64.StdEncoding.EncodeToString(ciphertext) != envelope.Ciphertext {
		return ErrInvalidEnvelope
	}
	return nil
}

func deriveKey(inputs [][2][32]byte) ([32]byte, error) {
	ikm := make([]byte, 32, 32+len(inputs)*32)
	for i := range ikm {
		ikm[i] = 0xff
	}
	defer func() { clear(ikm) }()
	for _, input := range inputs {
		private, err := ecdh.X25519().NewPrivateKey(input[0][:])
		if err != nil {
			return [32]byte{}, ErrInvalidKey
		}
		public, err := ecdh.X25519().NewPublicKey(input[1][:])
		if err != nil {
			return [32]byte{}, ErrInvalidKey
		}
		dh, err := private.ECDH(public)
		if err != nil {
			return [32]byte{}, ErrInvalidKey
		}
		ikm = append(ikm, dh...)
		clear(dh)
	}
	key, err := hkdf.Key(sha256.New, ikm, make([]byte, 32), "Mini-Hermes/X3DH/v1", 32)
	if err != nil {
		return [32]byte{}, ErrCrypto
	}
	defer clear(key)
	return [32]byte(key), nil
}

func makeAAD(ctx MessageContext, senderIK, recipientIK [32]byte, envelope Envelope) ([]byte, error) {
	aad := append(encodePublic(senderIK), encodePublic(recipientIK)...)
	aad = append(aad, []byte("Mini-Hermes/e2ee_v1\x00")...)
	for _, value := range []string{ctx.ThreadID, ctx.MessageID, ctx.SenderID, ctx.RecipientID} {
		id, err := canonicalUUID(value)
		if err != nil {
			return nil, err
		}
		aad = append(aad, id[:]...)
	}
	ephemeral, err := publicKey(envelope.EphemeralKey)
	if err != nil {
		return nil, err
	}
	aad = append(aad, encodePublic(ephemeral)...)
	aad = binary.BigEndian.AppendUint64(aad, uint64(envelope.SignedPrekeyID))
	var opk uint64
	if envelope.OneTimePrekeyID != nil {
		opk = uint64(*envelope.OneTimePrekeyID)
	}
	return binary.BigEndian.AppendUint64(aad, opk), nil
}

func newGCM(key [32]byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrCrypto
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCrypto
	}
	return aead, nil
}
