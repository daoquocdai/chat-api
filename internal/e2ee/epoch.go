package e2ee

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
)

const epochConfirmation = "Mini-Hermes/epoch-confirmed/v2"

func validateEpochContext(ctx EpochContext) error {
	for _, id := range []string{ctx.ThreadID, ctx.EpochID, ctx.SenderID, ctx.RecipientID} {
		if _, err := canonicalUUID(id); err != nil {
			return err
		}
	}
	if ctx.SenderID == ctx.RecipientID {
		return ErrInvalidContext
	}
	return nil
}

func epochContext(header EpochHeader) EpochContext {
	return EpochContext{ThreadID: header.ThreadID, EpochID: header.EpochID, SenderID: header.SenderID, RecipientID: header.RecipientID}
}

// Public header bytes are bound to confirmation and subsequent backups. UUIDs
// and fixed-size keys have unambiguous binary encodings.
func epochAAD(header EpochHeader) ([]byte, error) {
	if header.Version != 2 || validateEpochContext(epochContext(header)) != nil || header.SignedPrekeyID != 1 {
		return nil, ErrInvalidEnvelope
	}
	if id := header.OneTimePrekeyID; id != nil && (*id < 2 || *id > InitialOneTimePrekeys+1) {
		return nil, ErrInvalidEnvelope
	}
	aad := []byte("Mini-Hermes/key-epoch/v2\x00")
	for _, value := range []string{header.ThreadID, header.EpochID, header.SenderID, header.RecipientID} {
		id, _ := canonicalUUID(value)
		aad = append(aad, id[:]...)
	}
	for _, value := range []string{header.SenderIdentityKey, header.RecipientIdentityKey, header.EphemeralKey} {
		public, err := publicKey(value)
		if err != nil {
			return nil, ErrInvalidEnvelope
		}
		aad = append(aad, encodePublic(public)...)
	}
	aad = binary.BigEndian.AppendUint64(aad, uint64(header.SignedPrekeyID))
	var opk uint64
	if header.OneTimePrekeyID != nil {
		opk = uint64(*header.OneTimePrekeyID)
	}
	return binary.BigEndian.AppendUint64(aad, opk), nil
}

func ValidateEpochHeader(header EpochHeader) error {
	if _, err := epochAAD(header); err != nil {
		return err
	}
	if _, err := decode64(header.Nonce, 12); err != nil {
		return ErrInvalidEnvelope
	}
	if _, err := canonicalCiphertext(header.Ciphertext, len(epochConfirmation)+16, len(epochConfirmation)+16); err != nil {
		return ErrInvalidEnvelope
	}
	return nil
}

func ParseEpochHeader(content string) (EpochHeader, error) {
	if len(content) > 8192 {
		return EpochHeader{}, ErrInvalidEnvelope
	}
	var header EpochHeader
	if err := decodeExact([]byte(content), []string{"version", "thread_id", "epoch_id", "sender_id", "recipient_id",
		"sender_identity_key", "recipient_identity_key", "ephemeral_key", "signed_prekey_id", "one_time_prekey_id", "nonce", "ciphertext"},
		[]string{"one_time_prekey_id"}, &header); err != nil {
		return EpochHeader{}, err
	}
	if err := ValidateEpochHeader(header); err != nil {
		return EpochHeader{}, err
	}
	return header, nil
}

func EncodeEpochHeader(header EpochHeader) (string, error) {
	if err := ValidateEpochHeader(header); err != nil {
		return "", err
	}
	content, err := json.Marshal(header)
	if err != nil {
		return "", ErrCrypto
	}
	return string(content), nil
}

func EpochHeaderDigest(header EpochHeader) (string, error) {
	content, err := EncodeEpochHeader(header)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:]), nil
}

func confirmationKey(sessionKey [32]byte, aad []byte) ([32]byte, error) {
	if sessionKey == [32]byte{} {
		return [32]byte{}, ErrInvalidKey
	}
	key, err := hkdf.Key(sha256.New, sessionKey[:], nil, "Mini-Hermes/epoch-confirmation-key/v2\x00"+string(aad), 32)
	if err != nil {
		return [32]byte{}, ErrCrypto
	}
	defer clear(key)
	return [32]byte(key), nil
}

func CreateEpoch(ctx EpochContext, identity KeyPair, recipient Bundle) (EpochHeader, [32]byte, error) {
	if validateEpochContext(ctx) != nil || ctx.RecipientID != recipient.UserID {
		return EpochHeader{}, [32]byte{}, ErrInvalidContext
	}
	if err := validatePair(identity); err != nil {
		return EpochHeader{}, [32]byte{}, err
	}
	if err := VerifyBundle(recipient); err != nil {
		return EpochHeader{}, [32]byte{}, err
	}
	ephemeral, err := GenerateKeyPair()
	if err != nil {
		return EpochHeader{}, [32]byte{}, err
	}
	defer clear(ephemeral.Private[:])
	recipientIK, _ := publicKey(recipient.IdentityPublicKey)
	signed, _ := publicKey(recipient.SignedPrekey.PublicKey)
	inputs := [][2][32]byte{{identity.Private, signed}, {ephemeral.Private, recipientIK}, {ephemeral.Private, signed}}
	header := EpochHeader{Version: 2, ThreadID: ctx.ThreadID, EpochID: ctx.EpochID, SenderID: ctx.SenderID, RecipientID: ctx.RecipientID,
		SenderIdentityKey: base64.StdEncoding.EncodeToString(identity.Public[:]), RecipientIdentityKey: recipient.IdentityPublicKey,
		EphemeralKey: base64.StdEncoding.EncodeToString(ephemeral.Public[:]), SignedPrekeyID: recipient.SignedPrekey.KeyID}
	if opk := recipient.OneTimePrekey; opk != nil {
		public, _ := publicKey(opk.PublicKey)
		inputs = append(inputs, [2][32]byte{ephemeral.Private, public})
		id := opk.KeyID
		header.OneTimePrekeyID = &id
	}
	sessionKey, err := deriveKey(inputs)
	if err != nil {
		return EpochHeader{}, [32]byte{}, err
	}
	aad, err := epochAAD(header)
	if err != nil {
		return EpochHeader{}, [32]byte{}, err
	}
	key, err := confirmationKey(sessionKey, aad)
	if err != nil {
		return EpochHeader{}, [32]byte{}, err
	}
	defer clear(key[:])
	confirmation, err := sealRecord(key, []byte(epochConfirmation), aad)
	if err != nil {
		return EpochHeader{}, [32]byte{}, err
	}
	header.Nonce, header.Ciphertext = confirmation.Nonce, confirmation.Ciphertext
	return header, sessionKey, nil
}

func VerifyEpoch(sessionKey [32]byte, header EpochHeader) error {
	if err := ValidateEpochHeader(header); err != nil {
		return err
	}
	aad, _ := epochAAD(header)
	key, err := confirmationKey(sessionKey, aad)
	if err != nil {
		return err
	}
	defer clear(key[:])
	plaintext, err := openRecord(key, EncryptedRecord{Version: 1, Nonce: header.Nonce, Ciphertext: header.Ciphertext}, aad, len(epochConfirmation)+16)
	if err != nil {
		return err
	}
	defer clear(plaintext)
	if !bytes.Equal(plaintext, []byte(epochConfirmation)) {
		return ErrDecrypt
	}
	return nil
}

func OpenEpoch(ctx EpochContext, identity, signed KeyPair, oneTime *KeyPair, header EpochHeader) ([32]byte, error) {
	if validateEpochContext(ctx) != nil || ctx != epochContext(header) {
		return [32]byte{}, ErrInvalidContext
	}
	if err := ValidateEpochHeader(header); err != nil {
		return [32]byte{}, err
	}
	if validatePair(identity) != nil || validatePair(signed) != nil || (oneTime != nil && validatePair(*oneTime) != nil) ||
		header.RecipientIdentityKey != base64.StdEncoding.EncodeToString(identity.Public[:]) {
		return [32]byte{}, ErrInvalidKey
	}
	if (header.OneTimePrekeyID == nil) != (oneTime == nil) {
		return [32]byte{}, ErrInvalidKey
	}
	sender, _ := publicKey(header.SenderIdentityKey)
	ephemeral, _ := publicKey(header.EphemeralKey)
	inputs := [][2][32]byte{{signed.Private, sender}, {identity.Private, ephemeral}, {signed.Private, ephemeral}}
	if oneTime != nil {
		inputs = append(inputs, [2][32]byte{oneTime.Private, ephemeral})
	}
	sessionKey, err := deriveKey(inputs)
	if err != nil {
		return [32]byte{}, err
	}
	if err := VerifyEpoch(sessionKey, header); err != nil {
		clear(sessionKey[:])
		return [32]byte{}, err
	}
	return sessionKey, nil
}

func backupAAD(ownerID string, header EpochHeader) ([]byte, error) {
	owner, err := canonicalUUID(ownerID)
	if err != nil || (ownerID != header.SenderID && ownerID != header.RecipientID) {
		return nil, ErrInvalidContext
	}
	digest, err := EpochHeaderDigest(header)
	if err != nil {
		return nil, err
	}
	thread, _ := canonicalUUID(header.ThreadID)
	epoch, _ := canonicalUUID(header.EpochID)
	hash, _ := hex.DecodeString(digest)
	aad := append([]byte("Mini-Hermes/epoch-backup/v2\x00"), owner[:]...)
	aad = append(aad, thread[:]...)
	aad = append(aad, epoch[:]...)
	return append(aad, hash...), nil
}

func epochBackupKey(vaultKey [32]byte) ([32]byte, error) {
	if vaultKey == [32]byte{} {
		return [32]byte{}, ErrInvalidKey
	}
	key, err := hkdf.Key(sha256.New, vaultKey[:], nil, "Mini-Hermes/epoch-backup-key/v2", 32)
	if err != nil {
		return [32]byte{}, ErrCrypto
	}
	defer clear(key)
	return [32]byte(key), nil
}

func EncryptEpochBackup(vaultKey [32]byte, ownerID string, header EpochHeader, sessionKey [32]byte) (EncryptedRecord, error) {
	if err := VerifyEpoch(sessionKey, header); err != nil {
		return EncryptedRecord{}, err
	}
	aad, err := backupAAD(ownerID, header)
	if err != nil {
		return EncryptedRecord{}, err
	}
	key, err := epochBackupKey(vaultKey)
	if err != nil {
		return EncryptedRecord{}, err
	}
	defer clear(key[:])
	return sealRecord(key, sessionKey[:], aad)
}

func OpenEpochBackup(vaultKey [32]byte, ownerID string, header EpochHeader, record EncryptedRecord) ([32]byte, error) {
	aad, err := backupAAD(ownerID, header)
	if err != nil {
		return [32]byte{}, err
	}
	key, err := epochBackupKey(vaultKey)
	if err != nil {
		return [32]byte{}, err
	}
	defer clear(key[:])
	plaintext, err := openRecord(key, record, aad, 48)
	if err != nil {
		return [32]byte{}, err
	}
	defer clear(plaintext)
	if len(plaintext) != 32 {
		return [32]byte{}, ErrInvalidVault
	}
	sessionKey := [32]byte(plaintext)
	if err := VerifyEpoch(sessionKey, header); err != nil {
		clear(sessionKey[:])
		return [32]byte{}, err
	}
	return sessionKey, nil
}
