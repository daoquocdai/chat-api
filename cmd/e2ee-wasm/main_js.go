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
	"syscall/js"
	"unicode/utf8"

	"github.com/daoquocdai/chat-api/internal/e2ee"
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
		"verifyBundle":  verifyBundle,
		"createAccount": createAccount, "deriveCredentials": deriveCredentials,
		"openAccount": openAccount, "createEpoch": createEpoch, "openEpoch": openEpoch,
		"encryptEpochBackup": encryptEpochBackup, "decryptEpochBackup": decryptEpochBackup,
		"sealMessage": sealMessage, "openMessage": openMessage,
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
		case errors.Is(err, errInput), errors.Is(err, e2ee.ErrInvalidEnvelope), errors.Is(err, e2ee.ErrInvalidPlaintext),
			errors.Is(err, e2ee.ErrInvalidKDF), errors.Is(err, e2ee.ErrInvalidVault):
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
