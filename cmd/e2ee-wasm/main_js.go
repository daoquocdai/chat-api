//go:build js && wasm

// The bridge performs crypto/encoding only. HTTP, JWT, DOM and durable state
// belong to the web controller, not this boundary.
package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"syscall/js"
	"unicode/utf8"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/google/uuid"
)

var errInput = errors.New("invalid input")

type keyPairDTO struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

type bridgeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type result struct {
	OK    bool         `json:"ok"`
	Data  any          `json:"data"`
	Error *bridgeError `json:"error"`
}

// Hold the function handles for the lifetime of main; no user keys are retained.
var callbacks []js.Func

func main() {
	bridge := js.Global().Get("Object").New()
	methods := map[string]func([]byte) (any, error){
		"generateKeyPair": generateKeyPair, "signPrekey": signPrekey,
		"verifyBundle": verifyBundle, "seal": seal,
		"open": open, "decryptWithKey": decryptWithKey,
	}
	for name, method := range methods {
		callback := js.FuncOf(func(_ js.Value, args []js.Value) (output any) {
			defer func() {
				if recover() != nil {
					output = response(nil, e2ee.ErrCrypto)
				}
			}()
			if len(args) != 1 || args[0].Type() != js.TypeString {
				return response(nil, errInput)
			}
			input := args[0].String()
			if len(input) > 32768 || !utf8.ValidString(input) {
				return response(nil, errInput)
			}
			data, err := method([]byte(input))
			return response(data, err)
		})
		callbacks = append(callbacks, callback)
		bridge.Set(name, callback)
	}
	js.Global().Set("MiniHermesE2EE", bridge)
	select {}
}

func response(data any, err error) string {
	r := result{OK: err == nil, Data: data}
	if err != nil {
		code, message := "crypto_failed", "Không thể xử lý crypto."
		switch {
		case errors.Is(err, errInput), errors.Is(err, e2ee.ErrInvalidEnvelope), errors.Is(err, e2ee.ErrInvalidPlaintext):
			code, message = "invalid_input", "Yêu cầu crypto không hợp lệ."
		case errors.Is(err, e2ee.ErrInvalidKey):
			code, message = "invalid_key", "Khóa crypto không hợp lệ hoặc bị thiếu."
		case errors.Is(err, e2ee.ErrInvalidBundle):
			code, message = "invalid_bundle", "Bundle public không hợp lệ."
		case errors.Is(err, e2ee.ErrInvalidContext):
			code, message = "invalid_context", "Ngữ cảnh message không khớp."
		case errors.Is(err, e2ee.ErrDecrypt):
			code, message = "decrypt_failed", "Không thể giải mã message."
		}
		r.Data, r.Error = nil, &bridgeError{Code: code, Message: message}
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return `{"ok":false,"data":null,"error":{"code":"crypto_failed","message":"Không thể xử lý crypto."}}`
	}
	return string(encoded)
}

// Exact field names, required presence, duplicate rejection and one object.
// Optional nullable fields are checked by their individual DTO adapters.
func object(raw []byte, required, optional []string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return nil, errInput
	}
	allowed := make(map[string]bool, len(required)+len(optional))
	for _, key := range append(append([]string{}, required...), optional...) {
		allowed[key] = true
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] || fields[key] != nil {
			return nil, errInput
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, errInput
		}
		fields[key] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return nil, errInput
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errInput
	}
	for _, key := range required {
		if fields[key] == nil {
			return nil, errInput
		}
	}
	return fields, nil
}

func text(raw json.RawMessage) (string, error) {
	var value string
	if isNull(raw) || json.Unmarshal(raw, &value) != nil {
		return "", errInput
	}
	return value, nil
}

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func key32(raw json.RawMessage) ([32]byte, error) {
	value, err := text(raw)
	if err != nil {
		return [32]byte{}, err
	}
	if len(value) != base64.StdEncoding.EncodedLen(32) {
		return [32]byte{}, e2ee.ErrInvalidKey
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != 32 || base64.StdEncoding.EncodeToString(decoded) != value {
		return [32]byte{}, e2ee.ErrInvalidKey
	}
	return [32]byte(decoded), nil
}

func keyPair(raw json.RawMessage) (e2ee.KeyPair, error) {
	fields, err := object(raw, []string{"private_key", "public_key"}, nil)
	if err != nil {
		return e2ee.KeyPair{}, err
	}
	private, err := key32(fields["private_key"])
	if err != nil {
		return e2ee.KeyPair{}, err
	}
	public, err := key32(fields["public_key"])
	if err != nil {
		return e2ee.KeyPair{}, err
	}
	key, err := ecdh.X25519().NewPrivateKey(private[:])
	if err != nil || !bytes.Equal(key.PublicKey().Bytes(), public[:]) || e2ee.ValidatePublicKey(public) != nil {
		return e2ee.KeyPair{}, e2ee.ErrInvalidKey
	}
	return e2ee.KeyPair{Private: private, Public: public}, nil
}

func context(raw json.RawMessage) (e2ee.MessageContext, error) {
	keys := []string{"thread_id", "message_id", "sender_id", "recipient_id"}
	fields, err := object(raw, keys, nil)
	if err != nil {
		return e2ee.MessageContext{}, err
	}
	values := make([]string, len(keys))
	for i, key := range keys {
		value, err := text(fields[key])
		id, parseErr := uuid.Parse(value)
		if err != nil || parseErr != nil || id == uuid.Nil || id.String() != value {
			return e2ee.MessageContext{}, e2ee.ErrInvalidContext
		}
		values[i] = value
	}
	return e2ee.MessageContext{ThreadID: values[0], MessageID: values[1], SenderID: values[2], RecipientID: values[3]}, nil
}

func bundle(raw json.RawMessage) (e2ee.Bundle, error) {
	fields, err := object(raw, []string{"user_id", "identity_public_key", "signed_prekey", "one_time_prekey"}, nil)
	if err != nil {
		return e2ee.Bundle{}, err
	}
	if _, err := object(fields["signed_prekey"], []string{"key_id", "public_key", "signature"}, nil); err != nil {
		return e2ee.Bundle{}, err
	}
	if !isNull(fields["one_time_prekey"]) {
		if _, err := object(fields["one_time_prekey"], []string{"key_id", "public_key"}, nil); err != nil {
			return e2ee.Bundle{}, err
		}
	}
	var value e2ee.Bundle
	if json.Unmarshal(raw, &value) != nil {
		return e2ee.Bundle{}, errInput
	}
	if err := e2ee.VerifyBundle(value); err != nil {
		return e2ee.Bundle{}, err
	}
	return value, nil
}

func dto(pair e2ee.KeyPair) keyPairDTO {
	return keyPairDTO{PrivateKey: base64.StdEncoding.EncodeToString(pair.Private[:]), PublicKey: base64.StdEncoding.EncodeToString(pair.Public[:])}
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func fingerprint(encoded string) string {
	public, _ := base64.StdEncoding.DecodeString(encoded) // already validated
	return digest(public)
}

func generateKeyPair(raw []byte) (any, error) {
	fields, err := object(raw, nil, []string{"private_key"})
	if err != nil {
		return nil, err
	}
	var pair e2ee.KeyPair
	if value, exists := fields["private_key"]; exists {
		pair.Private, err = key32(value)
		if err != nil {
			return nil, err
		}
		private, err := ecdh.X25519().NewPrivateKey(pair.Private[:])
		if err != nil {
			return nil, e2ee.ErrInvalidKey
		}
		copy(pair.Public[:], private.PublicKey().Bytes())
	} else {
		pair, err = e2ee.GenerateKeyPair()
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"key_pair": dto(pair), "fingerprint": digest(pair.Public[:])}, nil
}

func signPrekey(raw []byte) (any, error) {
	fields, err := object(raw, []string{"identity_private_key", "signed_prekey_public_key"}, nil)
	if err != nil {
		return nil, err
	}
	identity, err := key32(fields["identity_private_key"])
	if err != nil {
		return nil, err
	}
	signed, err := key32(fields["signed_prekey_public_key"])
	if err != nil {
		return nil, err
	}
	signature, err := e2ee.SignPrekey(identity, signed)
	if err != nil {
		return nil, err
	}
	return map[string]any{"signature": base64.StdEncoding.EncodeToString(signature[:])}, nil
}

func verifyBundle(raw []byte) (any, error) {
	fields, err := object(raw, []string{"bundle"}, nil)
	if err != nil {
		return nil, err
	}
	value, err := bundle(fields["bundle"])
	if err != nil {
		return nil, err
	}
	return map[string]any{"valid": true, "fingerprint": fingerprint(value.IdentityPublicKey)}, nil
}

func seal(raw []byte) (any, error) {
	fields, err := object(raw, []string{"context", "identity", "bundle", "plaintext"}, nil)
	if err != nil {
		return nil, err
	}
	ctx, err := context(fields["context"])
	if err != nil {
		return nil, err
	}
	identity, err := keyPair(fields["identity"])
	if err != nil {
		return nil, err
	}
	recipient, err := bundle(fields["bundle"])
	if err != nil {
		return nil, err
	}
	plaintext, err := text(fields["plaintext"])
	if err != nil {
		return nil, err
	}
	envelope, key, err := e2ee.Seal(ctx, identity, recipient, []byte(plaintext))
	if err != nil {
		return nil, err
	}
	content, err := e2ee.EncodeEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": content, "message_key": base64.StdEncoding.EncodeToString(key[:]), "content_sha256": digest([]byte(content))}, nil
}

func open(raw []byte) (any, error) {
	fields, err := object(raw, []string{"context", "identity", "signed_prekey", "one_time_prekey", "content"}, nil)
	if err != nil {
		return nil, err
	}
	ctx, err := context(fields["context"])
	if err != nil {
		return nil, err
	}
	identity, err := keyPair(fields["identity"])
	if err != nil {
		return nil, err
	}
	signed, err := keyPair(fields["signed_prekey"])
	if err != nil {
		return nil, err
	}
	var oneTime *e2ee.KeyPair
	if !isNull(fields["one_time_prekey"]) {
		pair, err := keyPair(fields["one_time_prekey"])
		if err != nil {
			return nil, err
		}
		oneTime = &pair
	}
	content, err := text(fields["content"])
	if err != nil {
		return nil, err
	}
	envelope, err := e2ee.ParseEnvelope(content)
	if err != nil {
		return nil, err
	}
	plaintext, key, err := e2ee.Open(ctx, identity, signed, oneTime, envelope)
	if err != nil {
		return nil, err
	}
	return map[string]any{"plaintext": string(plaintext), "message_key": base64.StdEncoding.EncodeToString(key[:]), "content_sha256": digest([]byte(content)), "sender_fingerprint": fingerprint(envelope.SenderIdentityKey)}, nil
}

func decryptWithKey(raw []byte) (any, error) {
	fields, err := object(raw, []string{"context", "sender_identity_key", "recipient_identity_key", "message_key", "expected_content_sha256", "content"}, nil)
	if err != nil {
		return nil, err
	}
	ctx, err := context(fields["context"])
	if err != nil {
		return nil, err
	}
	sender, err := key32(fields["sender_identity_key"])
	if err != nil {
		return nil, err
	}
	recipient, err := key32(fields["recipient_identity_key"])
	if err != nil {
		return nil, err
	}
	key, err := key32(fields["message_key"])
	if err != nil {
		return nil, err
	}
	expected, err := text(fields["expected_content_sha256"])
	if err != nil {
		return nil, err
	}
	if decoded, err := hex.DecodeString(expected); err != nil || len(decoded) != 32 || strings.ToLower(expected) != expected {
		return nil, errInput
	}
	content, err := text(fields["content"])
	if err != nil {
		return nil, err
	}
	envelope, err := e2ee.ParseEnvelope(content)
	if err != nil {
		return nil, err
	}
	if digest([]byte(content)) != expected {
		return nil, e2ee.ErrInvalidContext
	}
	plaintext, err := e2ee.DecryptWithKey(ctx, sender, recipient, key, envelope)
	if err != nil {
		return nil, err
	}
	return map[string]any{"plaintext": string(plaintext)}, nil
}
