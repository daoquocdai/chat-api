package e2ee

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
)

func messageAAD(ctx EpochMessageContext) ([]byte, error) {
	aad := []byte("Mini-Hermes/message/v2\x00")
	for _, value := range []string{ctx.ThreadID, ctx.EpochID, ctx.MessageID, ctx.SenderID, ctx.RecipientID} {
		id, err := canonicalUUID(value)
		if err != nil {
			return nil, err
		}
		aad = append(aad, id[:]...)
	}
	if ctx.SenderID == ctx.RecipientID {
		return nil, ErrInvalidContext
	}
	return aad, nil
}

// This stateless KDF has no mutable chain/counter or Double Ratchet state.
// Separate UUID contexts and directions derive independent message keys.
func DeriveMessageKey(ctx EpochMessageContext, sessionKey [32]byte) ([32]byte, error) {
	if sessionKey == [32]byte{} {
		return [32]byte{}, ErrInvalidKey
	}
	aad, err := messageAAD(ctx)
	if err != nil {
		return [32]byte{}, err
	}
	key, err := hkdf.Key(sha256.New, sessionKey[:], nil, "Mini-Hermes/message-key/v2\x00"+string(aad), 32)
	if err != nil {
		return [32]byte{}, ErrCrypto
	}
	defer clear(key)
	return [32]byte(key), nil
}

func ValidateMessageEnvelope(envelope MessageEnvelope) error {
	if envelope.Version != 2 {
		return ErrInvalidEnvelope
	}
	for _, value := range []string{envelope.EpochID, envelope.RecipientID} {
		if _, err := canonicalUUID(value); err != nil {
			return ErrInvalidEnvelope
		}
	}
	if _, err := decode64(envelope.Nonce, 12); err != nil {
		return ErrInvalidEnvelope
	}
	if _, err := canonicalCiphertext(envelope.Ciphertext, 17, 4016); err != nil {
		return ErrInvalidEnvelope
	}
	return nil
}

func ParseMessageEnvelope(content string) (MessageEnvelope, error) {
	if len(content) > 8192 {
		return MessageEnvelope{}, ErrInvalidEnvelope
	}
	var envelope MessageEnvelope
	if err := decodeExact([]byte(content), []string{"version", "epoch_id", "recipient_id", "nonce", "ciphertext"}, nil, &envelope); err != nil {
		return MessageEnvelope{}, err
	}
	if err := ValidateMessageEnvelope(envelope); err != nil {
		return MessageEnvelope{}, err
	}
	return envelope, nil
}

func EncodeMessageEnvelope(envelope MessageEnvelope) (string, error) {
	if err := ValidateMessageEnvelope(envelope); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return "", ErrCrypto
	}
	return string(encoded), nil
}

func SealMessage(ctx EpochMessageContext, sessionKey [32]byte, plaintext []byte) (MessageEnvelope, error) {
	if err := validatePlaintext(plaintext); err != nil {
		return MessageEnvelope{}, err
	}
	key, err := DeriveMessageKey(ctx, sessionKey)
	if err != nil {
		return MessageEnvelope{}, err
	}
	defer clear(key[:])
	aad, _ := messageAAD(ctx)
	record, err := sealRecord(key, plaintext, aad)
	if err != nil {
		return MessageEnvelope{}, err
	}
	return MessageEnvelope{Version: 2, EpochID: ctx.EpochID, RecipientID: ctx.RecipientID,
		Nonce: record.Nonce, Ciphertext: record.Ciphertext}, nil
}

func OpenMessage(ctx EpochMessageContext, sessionKey [32]byte, envelope MessageEnvelope) ([]byte, error) {
	if err := ValidateMessageEnvelope(envelope); err != nil {
		return nil, err
	}
	if ctx.EpochID != envelope.EpochID || ctx.RecipientID != envelope.RecipientID {
		return nil, ErrInvalidContext
	}
	key, err := DeriveMessageKey(ctx, sessionKey)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	aad, _ := messageAAD(ctx)
	plaintext, err := openRecord(key, EncryptedRecord{Version: 1, Nonce: envelope.Nonce, Ciphertext: envelope.Ciphertext}, aad, 4016)
	if err != nil {
		return nil, err
	}
	if err := validatePlaintext(plaintext); err != nil {
		clear(plaintext)
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
