package e2ee

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func testEpochContext() EpochContext {
	return EpochContext{ThreadID: testContext().ThreadID, EpochID: "33333333-3333-4333-8333-333333333333", SenderID: aliceID, RecipientID: bobID}
}

func TestPasswordSeparationAndPortableAccountVault(t *testing.T) {
	profile, err := NewKDFProfile()
	if err != nil {
		t.Fatal("profile generation failed")
	}
	keys, err := DeriveCredentials("  ALICE  ", "account password fixture", profile)
	if err != nil {
		t.Fatal("credential derivation failed")
	}
	restoredKeys, err := DeriveCredentials("alice", "account password fixture", profile)
	if err != nil || keys != restoredKeys || keys.AuthCredential == b64(keys.VaultKey[:]) {
		t.Fatal("normalized account credential is unstable or auth and vault key are not separated")
	}
	vault, public, err := GenerateAccountVault()
	if err != nil {
		t.Fatal("account generation failed")
	}
	if len(vault.OneTimePrekeys) != 20 || ValidatePublicBundle(public) != nil {
		t.Fatal("account lacks immutable initial private/public prekeys")
	}
	record, err := EncryptAccountVault(keys.VaultKey, "alice", profile, public, vault)
	if err != nil {
		t.Fatal("account encryption failed")
	}
	// A fresh device gets only the password and durable server wire objects.
	serialized, _ := json.Marshal(record)
	var downloaded EncryptedRecord
	json.Unmarshal(serialized, &downloaded)
	restored, err := OpenAccountVault(restoredKeys.VaultKey, "alice", profile, public, downloaded)
	if err != nil || restored.Identity != vault.Identity || restored.SignedPrekey != vault.SignedPrekey || len(restored.OneTimePrekeys) != 20 {
		t.Fatal("password-only portable restore failed")
	}
	for i := range vault.OneTimePrekeys {
		if restored.OneTimePrekeys[i] != vault.OneTimePrekeys[i] {
			t.Fatal("private OPK archive did not survive account restore")
		}
	}
	// A valid tag does not replace validation of restored private/public pairs.
	badVault := vault
	badVault.Identity.PrivateKey = EncodePrivateKeyPair(testPair(t)).PrivateKey
	badBytes, _ := json.Marshal(badVault)
	goodAAD, _ := vaultAAD("alice", profile, public)
	badRecord, _ := sealRecord(keys.VaultKey, badBytes, goodAAD)
	if opened, err := OpenAccountVault(keys.VaultKey, "alice", profile, public, badRecord); err == nil || opened.Version != 0 {
		t.Fatal("authenticated malformed private pair restored")
	}
	authBytes, _ := base64.StdEncoding.DecodeString(keys.AuthCredential)
	if opened, err := OpenAccountVault([32]byte(authBytes), "alice", profile, public, record); err == nil || opened.Version != 0 {
		t.Fatal("server-visible auth credential opened the vault")
	}
	wrongKeys, err := DeriveCredentials("alice", "different password fixture", profile)
	if err != nil {
		t.Fatal("wrong-password derivation failed")
	}
	if opened, err := OpenAccountVault(wrongKeys.VaultKey, "alice", profile, public, record); err == nil || opened.Version != 0 {
		t.Fatal("wrong password exposed private keys")
	}
	if opened, err := OpenAccountVault(keys.VaultKey, "bob", profile, public, record); err == nil || opened.Version != 0 {
		t.Fatal("vault moved to another account")
	}
	modifiedProfile := profile
	salt, _ := base64.StdEncoding.DecodeString(profile.Salt)
	salt[0] ^= 1
	modifiedProfile.Salt = b64(salt)
	if opened, err := OpenAccountVault(keys.VaultKey, "alice", modifiedProfile, public, record); err == nil || opened.Version != 0 {
		t.Fatal("vault KDF metadata was not authenticated")
	}
	modifiedPublic := public
	modifiedPublic.OneTimePrekeys = append([]PublicPrekey(nil), public.OneTimePrekeys...)
	otherPair := testPair(t)
	modifiedPublic.OneTimePrekeys[0].PublicKey = b64(otherPair.Public[:])
	if opened, err := OpenAccountVault(keys.VaultKey, "alice", profile, modifiedPublic, record); err == nil || opened.Version != 0 {
		t.Fatal("vault public registration was not authenticated")
	}
	// Array order is serialization detail, not a different key registration.
	for i, j := 0, len(public.OneTimePrekeys)-1; i < j; i, j = i+1, j-1 {
		public.OneTimePrekeys[i], public.OneTimePrekeys[j] = public.OneTimePrekeys[j], public.OneTimePrekeys[i]
	}
	if _, err := OpenAccountVault(keys.VaultKey, "alice", profile, public, record); err != nil {
		t.Fatal("same public registration in different ordering failed")
	}
}

func TestKDFRejectsUnapprovedProfilesBeforeWork(t *testing.T) {
	profile, _ := NewKDFProfile()
	for name, mutate := range map[string]func(*KDFProfile){
		"version": func(p *KDFProfile) { p.Version++ }, "algorithm": func(p *KDFProfile) { p.Algorithm = "argon2i" },
		"lower memory": func(p *KDFProfile) { p.MemoryKiB-- }, "huge memory": func(p *KDFProfile) { p.MemoryKiB = ^uint32(0) },
		"iterations": func(p *KDFProfile) { p.Iterations-- }, "parallelism": func(p *KDFProfile) { p.Parallelism-- },
		"salt": func(p *KDFProfile) { p.Salt = b64(make([]byte, 15)) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := profile
			mutate(&bad)
			if result, err := DeriveCredentials("alice", "account password fixture", bad); !errors.Is(err, ErrInvalidKDF) || result != (PasswordKeys{}) {
				t.Fatal("unsupported KDF returned a credential or allocated unapproved parameters")
			}
		})
	}
}

func TestEpoch3DH4DHAndStatelessReplies(t *testing.T) {
	sender, recipient, signed, opk := testPair(t), testPair(t), testPair(t), testPair(t)
	for _, withOPK := range []bool{false, true} {
		name := "3DH"
		var oneTime *KeyPair
		if withOPK {
			name, oneTime = "4DH", &opk
		}
		t.Run(name, func(t *testing.T) {
			ctx := testEpochContext()
			header, sessionKey, err := CreateEpoch(ctx, sender, testBundle(t, bobID, recipient, signed, oneTime))
			if err != nil {
				t.Fatal("epoch creation failed")
			}
			if sessionKey != independentKey(t, recipient, signed, oneTime, Envelope{SenderIdentityKey: header.SenderIdentityKey, EphemeralKey: header.EphemeralKey}) {
				t.Fatal("epoch X3DH differs from independent DH/HMAC assembly")
			}
			content, _ := EncodeEpochHeader(header)
			parsed, err := ParseEpochHeader(content)
			if err != nil {
				t.Fatal("epoch parse failed")
			}
			received, err := OpenEpoch(ctx, recipient, signed, oneTime, parsed)
			if err != nil || received != sessionKey {
				t.Fatal("recipient key confirmation failed")
			}
			if withOPK {
				if key, err := OpenEpoch(ctx, recipient, signed, nil, parsed); err == nil || key != [32]byte{} {
					t.Fatal("4DH was silently downgraded after private OPK loss")
				}
			}
			for i, text := range []string{"first message", "reply", strings.Repeat("😀", 1000)} {
				messageCtx := EpochMessageContext{ThreadID: ctx.ThreadID, EpochID: ctx.EpochID, MessageID: testContext().MessageID, SenderID: aliceID, RecipientID: bobID}
				if i == 1 {
					messageCtx.SenderID, messageCtx.RecipientID = bobID, aliceID
				}
				if i == 2 {
					messageCtx.MessageID = bobID
				}
				envelope, err := SealMessage(messageCtx, sessionKey, []byte(text))
				if err != nil {
					t.Fatal("message seal failed")
				}
				wire, _ := EncodeMessageEnvelope(envelope)
				parsedMessage, err := ParseMessageEnvelope(wire)
				if err != nil {
					t.Fatal("message parse failed")
				}
				plaintext, err := OpenMessage(messageCtx, received, parsedMessage)
				if err != nil || string(plaintext) != text {
					t.Fatal("stateless message/reply failed using same restored epoch")
				}
			}
			messageCtx := EpochMessageContext{ThreadID: ctx.ThreadID, EpochID: ctx.EpochID, MessageID: testContext().MessageID, SenderID: aliceID, RecipientID: bobID}
			originalKey, _ := DeriveMessageKey(messageCtx, sessionKey)
			for _, mutate := range []func(*EpochMessageContext){
				func(c *EpochMessageContext) { c.MessageID = aliceID }, func(c *EpochMessageContext) { c.EpochID = bobID },
				func(c *EpochMessageContext) { c.ThreadID = aliceID },
				func(c *EpochMessageContext) { c.SenderID, c.RecipientID = c.RecipientID, c.SenderID },
			} {
				other := messageCtx
				mutate(&other)
				if key, err := DeriveMessageKey(other, sessionKey); err != nil || key == originalKey {
					t.Fatal("message keys are not separated across UUID contexts and directions")
				}
			}
		})
	}
}

func TestEpochBackupsRestoreBothOwnersAndBindHeader(t *testing.T) {
	sender, recipient, signed, opk := testPair(t), testPair(t), testPair(t), testPair(t)
	header, sessionKey, err := CreateEpoch(testEpochContext(), sender, testBundle(t, bobID, recipient, signed, &opk))
	if err != nil {
		t.Fatal("epoch setup failed")
	}
	for _, owner := range []string{aliceID, bobID} {
		vaultKey := testPair(t).Private
		backup, err := EncryptEpochBackup(vaultKey, owner, header, sessionKey)
		if err != nil {
			t.Fatal("owner backup failed")
		}
		// Restore requires no ephemeral/private OPK/local message cache.
		restored, err := OpenEpochBackup(vaultKey, owner, header, backup)
		if err != nil || restored != sessionKey {
			t.Fatal("owner session restore failed")
		}
		wrongKey := vaultKey
		wrongKey[0] ^= 1
		if key, err := OpenEpochBackup(wrongKey, owner, header, backup); err == nil || key != [32]byte{} {
			t.Fatal("wrong vault key exposed session secret")
		}
		otherOwner := aliceID
		if owner == aliceID {
			otherOwner = bobID
		}
		if key, err := OpenEpochBackup(vaultKey, otherOwner, header, backup); err == nil || key != [32]byte{} {
			t.Fatal("backup moved to another owner")
		}
		changed := header
		changed.EpochID = aliceID
		if key, err := OpenEpochBackup(vaultKey, owner, changed, backup); err == nil || key != [32]byte{} {
			t.Fatal("backup moved to another epoch/bootstrap")
		}
		// Even a valid backup AEAD must not install an unrelated epoch secret.
		wrongSession := sessionKey
		wrongSession[0] ^= 1
		backupKey, _ := epochBackupKey(vaultKey)
		aad, _ := backupAAD(owner, header)
		incorrectBackup, _ := sealRecord(backupKey, wrongSession[:], aad)
		if key, err := OpenEpochBackup(vaultKey, owner, header, incorrectBackup); err == nil || key != [32]byte{} {
			t.Fatal("authenticated backup skipped epoch confirmation")
		}
	}
}

func TestMessageKDFIndependentByteLayout(t *testing.T) {
	ctx := EpochMessageContext{ThreadID: testContext().ThreadID, EpochID: testEpochContext().EpochID,
		MessageID: testContext().MessageID, SenderID: aliceID, RecipientID: bobID}
	want := append([]byte("Mini-Hermes/message/v2\x00"), unhex(t,
		"11111111111141118111111111111111"+"33333333333343338333333333333333"+
			"22222222222242228222222222222222"+"aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa"+"bbbbbbbbbbbb4bbb8bbbbbbbbbbbbbbb")...)
	aad, err := messageAAD(ctx)
	if err != nil || !bytes.Equal(aad, want) {
		t.Fatal("message context byte layout changed")
	}
	var session [32]byte
	for i := range session {
		session[i] = byte(i + 1)
	}
	// RFC 5869's extract/expand assembled without crypto/hkdf.
	extract := hmac.New(sha256.New, make([]byte, 32))
	extract.Write(session[:])
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write([]byte("Mini-Hermes/message-key/v2\x00"))
	expand.Write(want)
	expand.Write([]byte{1})
	got, err := DeriveMessageKey(ctx, session)
	if err != nil || !bytes.Equal(got[:], expand.Sum(nil)) {
		t.Fatal("message KDF differs from independent extract/expand")
	}
}

func TestEpochAndMessageTamperingNeverReturnsSecrets(t *testing.T) {
	sender, recipient, signed, opk := testPair(t), testPair(t), testPair(t), testPair(t)
	ctx := testEpochContext()
	header, sessionKey, _ := CreateEpoch(ctx, sender, testBundle(t, bobID, recipient, signed, &opk))
	flip := func(encoded string) string {
		data, _ := base64.StdEncoding.DecodeString(encoded)
		data[0] ^= 1
		return b64(data)
	}
	for name, mutate := range map[string]func(*EpochHeader){
		"thread": func(h *EpochHeader) { h.ThreadID = aliceID }, "epoch": func(h *EpochHeader) { h.EpochID = bobID },
		"sender": func(h *EpochHeader) { h.SenderID = ctx.ThreadID }, "recipient": func(h *EpochHeader) { h.RecipientID = ctx.ThreadID },
		"sender identity":    func(h *EpochHeader) { pair := testPair(t); h.SenderIdentityKey = b64(pair.Public[:]) },
		"recipient identity": func(h *EpochHeader) { pair := testPair(t); h.RecipientIdentityKey = b64(pair.Public[:]) },
		"ephemeral":          func(h *EpochHeader) { pair := testPair(t); h.EphemeralKey = b64(pair.Public[:]) },
		"OPK":                func(h *EpochHeader) { id := int64(3); h.OneTimePrekeyID = &id },
		"nonce":              func(h *EpochHeader) { h.Nonce = flip(h.Nonce) }, "confirmation": func(h *EpochHeader) { h.Ciphertext = flip(h.Ciphertext) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := header
			mutate(&bad)
			if key, err := OpenEpoch(epochContext(bad), recipient, signed, &opk, bad); err == nil || key != [32]byte{} {
				t.Fatal("modified epoch returned secret")
			}
			if VerifyEpoch(sessionKey, bad) == nil {
				t.Fatal("modified epoch confirmation accepted")
			}
		})
	}
	messageCtx := EpochMessageContext{ThreadID: ctx.ThreadID, EpochID: ctx.EpochID, MessageID: testContext().MessageID, SenderID: aliceID, RecipientID: bobID}
	envelope, _ := SealMessage(messageCtx, sessionKey, []byte("test ciphertext"))
	for name, mutate := range map[string]func(*EpochMessageContext, *MessageEnvelope){
		"thread":  func(c *EpochMessageContext, _ *MessageEnvelope) { c.ThreadID = aliceID },
		"epoch":   func(c *EpochMessageContext, e *MessageEnvelope) { c.EpochID, e.EpochID = aliceID, aliceID },
		"message": func(c *EpochMessageContext, _ *MessageEnvelope) { c.MessageID = aliceID },
		"direction": func(c *EpochMessageContext, e *MessageEnvelope) {
			c.SenderID, c.RecipientID = c.RecipientID, c.SenderID
			e.RecipientID = c.RecipientID
		},
		"nonce":      func(_ *EpochMessageContext, e *MessageEnvelope) { e.Nonce = flip(e.Nonce) },
		"ciphertext": func(_ *EpochMessageContext, e *MessageEnvelope) { e.Ciphertext = flip(e.Ciphertext) },
	} {
		t.Run(name, func(t *testing.T) {
			badCtx, bad := messageCtx, envelope
			mutate(&badCtx, &bad)
			if plain, err := OpenMessage(badCtx, sessionKey, bad); err == nil || plain != nil {
				t.Fatal("modified message exposed plaintext")
			}
		})
	}
	// A valid AEAD tag must not allow invalid UTF-8/plaintext to reach the UI.
	key, _ := DeriveMessageKey(messageCtx, sessionKey)
	aad, _ := messageAAD(messageCtx)
	badRecord, _ := sealRecord(key, []byte{0xff}, aad)
	bad := envelope
	bad.Nonce, bad.Ciphertext = badRecord.Nonce, badRecord.Ciphertext
	if plain, err := OpenMessage(messageCtx, sessionKey, bad); err == nil || plain != nil {
		t.Fatal("authenticated invalid text exposed plaintext")
	}
}

func TestV2WireStrictnessAndCanonicalBootstrapHash(t *testing.T) {
	sender, recipient, signed := testPair(t), testPair(t), testPair(t)
	header, key, _ := CreateEpoch(testEpochContext(), sender, testBundle(t, bobID, recipient, signed, nil))
	content, _ := EncodeEpochHeader(header)
	digest, _ := EpochHeaderDigest(header)
	var formatted bytes.Buffer
	json.Indent(&formatted, []byte(content), "", "  ")
	parsed, err := ParseEpochHeader(formatted.String())
	otherDigest, _ := EpochHeaderDigest(parsed)
	if err != nil || digest != otherDigest {
		t.Fatal("bootstrap digest depends on JSON presentation")
	}
	ctx := EpochMessageContext{ThreadID: header.ThreadID, EpochID: header.EpochID, MessageID: testContext().MessageID, SenderID: aliceID, RecipientID: bobID}
	envelope, _ := SealMessage(ctx, key, []byte("x"))
	message, _ := EncodeMessageEnvelope(envelope)
	for _, wire := range []string{
		message + "{}", strings.Replace(message, `"version"`, `"Version"`, 1), strings.Replace(message, `"version":2`, `"version":null`, 1),
		message[:len(message)-1] + `,"version":2}`, message[:len(message)-1] + `,"extra":1}`,
		strings.Replace(message, `"epoch_id":"`+envelope.EpochID+`",`, "", 1),
	} {
		if _, err := ParseMessageEnvelope(wire); err == nil {
			t.Fatal("ambiguous v2 message accepted")
		}
	}
	for _, wire := range []string{
		content + "{}", strings.Replace(content, `"sender_id"`, `"Sender_id"`, 1), content[:len(content)-1] + `,"version":2}`,
		strings.Replace(content, `"one_time_prekey_id":null,`, "", 1), content[:len(content)-1] + `,"extra":1}`,
	} {
		if _, err := ParseEpochHeader(wire); err == nil {
			t.Fatal("ambiguous epoch header accepted")
		}
	}
}
