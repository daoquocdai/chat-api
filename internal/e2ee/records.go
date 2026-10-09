package e2ee

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"unicode/utf8"
)

const MaxVaultCiphertextBytes = 16 * 1024

func ValidateEncryptedRecord(record EncryptedRecord, maxCiphertextBytes int) error {
	if record.Version != 1 || maxCiphertextBytes < 17 {
		return ErrInvalidVault
	}
	if _, err := decode64(record.Nonce, 12); err != nil {
		return ErrInvalidVault
	}
	if _, err := canonicalCiphertext(record.Ciphertext, 17, maxCiphertextBytes); err != nil {
		return ErrInvalidVault
	}
	return nil
}

func canonicalCiphertext(value string, minimum, maximum int) ([]byte, error) {
	if len(value) > base64.StdEncoding.EncodedLen(maximum) {
		return nil, ErrInvalidEnvelope
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) < minimum || len(decoded) > maximum || base64.StdEncoding.EncodeToString(decoded) != value {
		return nil, ErrInvalidEnvelope
	}
	return decoded, nil
}

func sealRecord(key [32]byte, plaintext, aad []byte) (EncryptedRecord, error) {
	if key == [32]byte{} {
		return EncryptedRecord{}, ErrInvalidKey
	}
	aead, err := newGCM(key)
	if err != nil {
		return EncryptedRecord{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedRecord{}, ErrCrypto
	}
	return EncryptedRecord{Version: 1, Nonce: base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, aad))}, nil
}

func openRecord(key [32]byte, record EncryptedRecord, aad []byte, maximum int) ([]byte, error) {
	if key == [32]byte{} {
		return nil, ErrInvalidKey
	}
	if err := ValidateEncryptedRecord(record, maximum); err != nil {
		return nil, err
	}
	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce, _ := decode64(record.Nonce, 12)
	ciphertext, _ := canonicalCiphertext(record.Ciphertext, 17, maximum)
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// decodeExact rejects ambiguous crypto wire objects before a typed decode.
func decodeExact(raw []byte, names, nullable []string, out any) error {
	if !utf8.Valid(raw) {
		return ErrInvalidEnvelope
	}
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = false
	}
	nullAllowed := make(map[string]bool, len(nullable))
	for _, name := range nullable {
		nullAllowed[name] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return ErrInvalidEnvelope
	}
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		seen, known := allowed[name]
		if err != nil || !ok || !known || seen {
			return ErrInvalidEnvelope
		}
		allowed[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || (!nullAllowed[name] && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
			return ErrInvalidEnvelope
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return ErrInvalidEnvelope
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrInvalidEnvelope
	}
	for _, seen := range allowed {
		if !seen {
			return ErrInvalidEnvelope
		}
	}
	if json.Unmarshal(raw, out) != nil {
		return ErrInvalidEnvelope
	}
	return nil
}
