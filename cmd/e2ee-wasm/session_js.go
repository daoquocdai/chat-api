//go:build js && wasm

package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/daoquocdai/chat-api/internal/e2ee"
)

func kdfProfile(raw json.RawMessage) (e2ee.KDFProfile, error) {
	if _, err := object(raw, []string{"version", "algorithm", "salt", "memory_kib", "iterations", "parallelism"}, nil); err != nil {
		return e2ee.KDFProfile{}, err
	}
	var profile e2ee.KDFProfile
	if json.Unmarshal(raw, &profile) != nil {
		return e2ee.KDFProfile{}, errInput
	}
	if err := e2ee.ValidateKDFProfile(profile); err != nil {
		return e2ee.KDFProfile{}, err
	}
	return profile, nil
}

func publicUpload(raw json.RawMessage) (e2ee.UploadRequest, error) {
	fields, err := object(raw, []string{"identity_public_key", "signed_prekey", "one_time_prekeys"}, nil)
	if err != nil {
		return e2ee.UploadRequest{}, err
	}
	if _, err := object(fields["signed_prekey"], []string{"key_id", "public_key", "signature"}, nil); err != nil {
		return e2ee.UploadRequest{}, err
	}
	var prekeys []json.RawMessage
	if json.Unmarshal(fields["one_time_prekeys"], &prekeys) != nil || len(prekeys) != e2ee.InitialOneTimePrekeys {
		return e2ee.UploadRequest{}, errInput
	}
	for _, prekey := range prekeys {
		if _, err := object(prekey, []string{"key_id", "public_key"}, nil); err != nil {
			return e2ee.UploadRequest{}, err
		}
	}
	var value e2ee.UploadRequest
	if json.Unmarshal(raw, &value) != nil {
		return e2ee.UploadRequest{}, errInput
	}
	if err := e2ee.ValidatePublicBundle(value); err != nil {
		return e2ee.UploadRequest{}, err
	}
	return value, nil
}

func encryptedRecord(raw json.RawMessage) (e2ee.EncryptedRecord, error) {
	if _, err := object(raw, []string{"version", "nonce", "ciphertext"}, nil); err != nil {
		return e2ee.EncryptedRecord{}, err
	}
	var value e2ee.EncryptedRecord
	if json.Unmarshal(raw, &value) != nil {
		return e2ee.EncryptedRecord{}, errInput
	}
	if err := e2ee.ValidateEncryptedRecord(value, e2ee.MaxVaultCiphertextBytes); err != nil {
		return e2ee.EncryptedRecord{}, err
	}
	return value, nil
}

func createAccount(raw []byte) (any, error) {
	fields, err := object(raw, []string{"username", "password"}, nil)
	if err != nil {
		return nil, err
	}
	username, err := text(fields["username"])
	if err != nil {
		return nil, err
	}
	password, err := text(fields["password"])
	if err != nil {
		return nil, err
	}
	profile, err := e2ee.NewKDFProfile()
	if err != nil {
		return nil, err
	}
	keys, err := e2ee.DeriveCredentials(username, password, profile)
	if err != nil {
		return nil, err
	}
	defer clear(keys.VaultKey[:])
	account, public, err := e2ee.GenerateAccountVault()
	if err != nil {
		return nil, err
	}
	encrypted, err := e2ee.EncryptAccountVault(keys.VaultKey, username, profile, public, account)
	if err != nil {
		return nil, err
	}
	return map[string]any{"kdf": profile, "auth_credential": keys.AuthCredential,
		"vault_key": base64.StdEncoding.EncodeToString(keys.VaultKey[:]), "public_bundle": public,
		"encrypted_vault": encrypted, "account": account, "fingerprint": fingerprint(public.IdentityPublicKey)}, nil
}

func deriveCredentials(raw []byte) (any, error) {
	fields, err := object(raw, []string{"username", "password", "kdf"}, nil)
	if err != nil {
		return nil, err
	}
	username, err := text(fields["username"])
	if err != nil {
		return nil, err
	}
	password, err := text(fields["password"])
	if err != nil {
		return nil, err
	}
	profile, err := kdfProfile(fields["kdf"])
	if err != nil {
		return nil, err
	}
	keys, err := e2ee.DeriveCredentials(username, password, profile)
	if err != nil {
		return nil, err
	}
	defer clear(keys.VaultKey[:])
	return map[string]any{"auth_credential": keys.AuthCredential, "vault_key": base64.StdEncoding.EncodeToString(keys.VaultKey[:])}, nil
}

func openAccount(raw []byte) (any, error) {
	fields, err := object(raw, []string{"username", "kdf", "vault_key", "public_bundle", "encrypted_vault"}, nil)
	if err != nil {
		return nil, err
	}
	username, err := text(fields["username"])
	if err != nil {
		return nil, err
	}
	profile, err := kdfProfile(fields["kdf"])
	if err != nil {
		return nil, err
	}
	key, err := key32(fields["vault_key"])
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	public, err := publicUpload(fields["public_bundle"])
	if err != nil {
		return nil, err
	}
	record, err := encryptedRecord(fields["encrypted_vault"])
	if err != nil {
		return nil, err
	}
	account, err := e2ee.OpenAccountVault(key, username, profile, public, record)
	if err != nil {
		return nil, err
	}
	return map[string]any{"account": account, "fingerprint": fingerprint(public.IdentityPublicKey)}, nil
}

func epochContextInput(raw json.RawMessage) (e2ee.EpochContext, error) {
	if _, err := object(raw, []string{"thread_id", "epoch_id", "sender_id", "recipient_id"}, nil); err != nil {
		return e2ee.EpochContext{}, err
	}
	var value e2ee.EpochContext
	if json.Unmarshal(raw, &value) != nil {
		return e2ee.EpochContext{}, errInput
	}
	return value, nil
}

func epochHeaderInput(raw json.RawMessage) (e2ee.EpochHeader, error) {
	value, err := text(raw)
	if err != nil {
		return e2ee.EpochHeader{}, err
	}
	return e2ee.ParseEpochHeader(value)
}

func epochData(key [32]byte, header e2ee.EpochHeader) map[string]any {
	return map[string]any{"session_key": base64.StdEncoding.EncodeToString(key[:]),
		"sender_fingerprint": fingerprint(header.SenderIdentityKey), "recipient_fingerprint": fingerprint(header.RecipientIdentityKey)}
}

func createEpoch(raw []byte) (any, error) {
	fields, err := object(raw, []string{"context", "identity", "bundle"}, nil)
	if err != nil {
		return nil, err
	}
	ctx, err := epochContextInput(fields["context"])
	if err != nil {
		return nil, err
	}
	identity, err := keyPair(fields["identity"])
	if err != nil {
		return nil, err
	}
	defer clear(identity.Private[:])
	recipient, err := bundle(fields["bundle"])
	if err != nil {
		return nil, err
	}
	header, key, err := e2ee.CreateEpoch(ctx, identity, recipient)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	content, err := e2ee.EncodeEpochHeader(header)
	if err != nil {
		return nil, err
	}
	data := epochData(key, header)
	data["header"] = content
	return data, nil
}

func openEpoch(raw []byte) (any, error) {
	fields, err := object(raw, []string{"context", "identity", "signed_prekey", "one_time_prekey", "header"}, nil)
	if err != nil {
		return nil, err
	}
	ctx, err := epochContextInput(fields["context"])
	if err != nil {
		return nil, err
	}
	identity, err := keyPair(fields["identity"])
	if err != nil {
		return nil, err
	}
	defer clear(identity.Private[:])
	signed, err := keyPair(fields["signed_prekey"])
	if err != nil {
		return nil, err
	}
	defer clear(signed.Private[:])
	var oneTime *e2ee.KeyPair
	if !isNull(fields["one_time_prekey"]) {
		pair, err := keyPair(fields["one_time_prekey"])
		if err != nil {
			return nil, err
		}
		oneTime = &pair
		defer clear(pair.Private[:])
	}
	header, err := epochHeaderInput(fields["header"])
	if err != nil {
		return nil, err
	}
	key, err := e2ee.OpenEpoch(ctx, identity, signed, oneTime, header)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	return epochData(key, header), nil
}

func encryptEpochBackup(raw []byte) (any, error) {
	fields, err := object(raw, []string{"vault_key", "owner_id", "header", "session_key"}, nil)
	if err != nil {
		return nil, err
	}
	vaultKey, err := key32(fields["vault_key"])
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey[:])
	owner, err := text(fields["owner_id"])
	if err != nil {
		return nil, err
	}
	header, err := epochHeaderInput(fields["header"])
	if err != nil {
		return nil, err
	}
	key, err := key32(fields["session_key"])
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	backup, err := e2ee.EncryptEpochBackup(vaultKey, owner, header, key)
	if err != nil {
		return nil, err
	}
	return map[string]any{"backup": backup}, nil
}

func decryptEpochBackup(raw []byte) (any, error) {
	fields, err := object(raw, []string{"vault_key", "owner_id", "header", "backup"}, nil)
	if err != nil {
		return nil, err
	}
	vaultKey, err := key32(fields["vault_key"])
	if err != nil {
		return nil, err
	}
	defer clear(vaultKey[:])
	owner, err := text(fields["owner_id"])
	if err != nil {
		return nil, err
	}
	header, err := epochHeaderInput(fields["header"])
	if err != nil {
		return nil, err
	}
	backup, err := encryptedRecord(fields["backup"])
	if err != nil {
		return nil, err
	}
	key, err := e2ee.OpenEpochBackup(vaultKey, owner, header, backup)
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	return epochData(key, header), nil
}

func messageContextInput(raw json.RawMessage) (e2ee.EpochMessageContext, error) {
	if _, err := object(raw, []string{"thread_id", "epoch_id", "message_id", "sender_id", "recipient_id"}, nil); err != nil {
		return e2ee.EpochMessageContext{}, err
	}
	var ctx e2ee.EpochMessageContext
	if json.Unmarshal(raw, &ctx) != nil {
		return e2ee.EpochMessageContext{}, errInput
	}
	return ctx, nil
}

func sealMessage(raw []byte) (any, error) {
	fields, err := object(raw, []string{"context", "session_key", "plaintext"}, nil)
	if err != nil {
		return nil, err
	}
	ctx, err := messageContextInput(fields["context"])
	if err != nil {
		return nil, err
	}
	key, err := key32(fields["session_key"])
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	plaintext, err := text(fields["plaintext"])
	if err != nil {
		return nil, err
	}
	envelope, err := e2ee.SealMessage(ctx, key, []byte(plaintext))
	if err != nil {
		return nil, err
	}
	content, err := e2ee.EncodeMessageEnvelope(envelope)
	if err != nil {
		return nil, err
	}
	return map[string]any{"content": content, "content_sha256": digest([]byte(content))}, nil
}

func openMessage(raw []byte) (any, error) {
	fields, err := object(raw, []string{"context", "session_key", "content"}, nil)
	if err != nil {
		return nil, err
	}
	ctx, err := messageContextInput(fields["context"])
	if err != nil {
		return nil, err
	}
	key, err := key32(fields["session_key"])
	if err != nil {
		return nil, err
	}
	defer clear(key[:])
	content, err := text(fields["content"])
	if err != nil {
		return nil, err
	}
	envelope, err := e2ee.ParseMessageEnvelope(content)
	if err != nil {
		return nil, err
	}
	plaintext, err := e2ee.OpenMessage(ctx, key, envelope)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	return map[string]any{"plaintext": string(plaintext), "content_sha256": digest([]byte(content))}, nil
}
