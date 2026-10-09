package e2ee

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go.mau.fi/libsignal/ecc"
)

const (
	aliceID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	bobID   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func testContext() MessageContext {
	return MessageContext{
		ThreadID:  "11111111-1111-4111-8111-111111111111",
		MessageID: "22222222-2222-4222-8222-222222222222",
		SenderID:  aliceID, RecipientID: bobID,
	}
}

func testPair(t *testing.T) KeyPair {
	t.Helper()
	pair, err := GenerateKeyPair()
	if err != nil {
		t.Fatal("key generation failed")
	}
	return pair
}

func testBundle(t *testing.T, user string, ik, spk KeyPair, opk *KeyPair) Bundle {
	t.Helper()
	signature, err := SignPrekey(ik.Private, spk.Public)
	if err != nil {
		t.Fatal("signing failed")
	}
	bundle := Bundle{UserID: user, IdentityPublicKey: b64(ik.Public[:]), SignedPrekey: SignedPrekey{KeyID: 1, PublicKey: b64(spk.Public[:]), Signature: b64(signature[:])}}
	if opk != nil {
		bundle.OneTimePrekey = &PublicPrekey{KeyID: 2, PublicKey: b64(opk.Public[:])}
	}
	return bundle
}

func b64(data []byte) string { return base64.StdEncoding.EncodeToString(data) }

func unhex(t *testing.T, encoded string) []byte {
	t.Helper()
	data, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal("invalid test vector")
	}
	return data
}

// RFC 7748 section 6.1 and RFC 5869 Appendix A.1/A.3.
func TestRFCVectors(t *testing.T) {
	a := unhex(t, "77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	b := unhex(t, "5dab087e624a8a4b79e17f8b83800ee66f3bb1292618b6fd1c2f8b27ff88e0eb")
	aPub := unhex(t, "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")
	bPub := unhex(t, "de9edb7d7b7dc1b4d35b61c2ece435373f8343c85b78674dadfc7e146f882b4f")
	want := unhex(t, "4a5d9d5ba4ce2de1728e3bf480350f25e07e21c947d19e3376f09b3c1e161742")
	for _, vector := range []struct{ private, public, peer []byte }{{a, aPub, bPub}, {b, bPub, aPub}} {
		private, err := ecdh.X25519().NewPrivateKey(vector.private)
		if err != nil || !bytes.Equal(private.PublicKey().Bytes(), vector.public) {
			t.Fatal("RFC 7748 public key mismatch")
		}
		peer, _ := ecdh.X25519().NewPublicKey(vector.peer)
		shared, err := private.ECDH(peer)
		if err != nil || !bytes.Equal(shared, want) {
			t.Fatal("RFC 7748 shared secret mismatch")
		}
	}
	for _, vector := range []struct{ salt, info, okm string }{
		{"000102030405060708090a0b0c", "f0f1f2f3f4f5f6f7f8f9", "3cb25f25faacd57a90434f64d0362f2a2d2d0a90cf1a5a4c5db02d56ecc4c5bf34007208d5b887185865"},
		{"", "", "8da4e775a563c18f715f802a063c5a31b8a11f5c5ee1879ec3454e5f3c738d2d9d201395faa4b61a96c8"},
	} {
		got, err := hkdf.Key(sha256.New, bytes.Repeat([]byte{0x0b}, 22), unhex(t, vector.salt), string(unhex(t, vector.info)), 42)
		if err != nil || !bytes.Equal(got, unhex(t, vector.okm)) {
			t.Fatal("RFC 5869 output mismatch")
		}
	}
}

// Independent receiver DH assembly and HMAC extract/expand catch ordering,
// F32, salt/info and optional-DH mistakes that symmetric round trips can hide.
func independentKey(t *testing.T, ik, spk KeyPair, opk *KeyPair, envelope Envelope) [32]byte {
	t.Helper()
	sender, _ := base64.StdEncoding.DecodeString(envelope.SenderIdentityKey)
	ek, _ := base64.StdEncoding.DecodeString(envelope.EphemeralKey)
	material := bytes.Repeat([]byte{0xff}, 32)
	dh := func(private [32]byte, public []byte) {
		priv, _ := ecdh.X25519().NewPrivateKey(private[:])
		pub, _ := ecdh.X25519().NewPublicKey(public)
		shared, err := priv.ECDH(pub)
		if err != nil {
			t.Fatal("fixture DH failed")
		}
		material = append(material, shared...)
	}
	dh(spk.Private, sender)
	dh(ik.Private, ek)
	dh(spk.Private, ek)
	if opk != nil {
		dh(opk.Private, ek)
	}
	extract := hmac.New(sha256.New, make([]byte, 32))
	extract.Write(material)
	expand := hmac.New(sha256.New, extract.Sum(nil))
	expand.Write([]byte("Mini-Hermes/X3DH/v1"))
	expand.Write([]byte{1})
	return [32]byte(expand.Sum(nil))
}

func TestMessages3DH4DHAndReverse(t *testing.T) {
	a, b, sa, sb, oa, ob := testPair(t), testPair(t), testPair(t), testPair(t), testPair(t), testPair(t)
	for _, withOPK := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			name := "3DH"
			if withOPK {
				name = "4DH"
			}
			if reverse {
				name += "/reverse"
			}
			t.Run(name, func(t *testing.T) {
				ctx, sender, receiver, signed, oneTime := testContext(), a, b, sb, ob
				if reverse {
					ctx.SenderID, ctx.RecipientID = bobID, aliceID
					sender, receiver, signed, oneTime = b, a, sa, oa
				}
				var opk *KeyPair
				if withOPK {
					opk = &oneTime
				}
				bundle := testBundle(t, ctx.RecipientID, receiver, signed, opk)
				for _, text := range []string{"Xin chào 🌍", "x", strings.Repeat("😀", 1000)} {
					envelope, key, err := Seal(ctx, sender, bundle, []byte(text))
					if err != nil {
						t.Fatal("seal failed")
					}
					if key != independentKey(t, receiver, signed, opk, envelope) {
						t.Fatal("X3DH profile mismatch")
					}
					content, err := EncodeEnvelope(envelope)
					if err != nil || len(content) > 8192 {
						t.Fatal("encoding failed")
					}
					parsed, err := ParseEnvelope(content)
					if err != nil {
						t.Fatal("parsing failed")
					}
					plaintext, receivedKey, err := Open(ctx, receiver, signed, opk, parsed)
					if err != nil || receivedKey != key || string(plaintext) != text {
						t.Fatal("open mismatch")
					}
					cached, err := DecryptWithKey(ctx, sender.Public, receiver.Public, receivedKey, parsed)
					if err != nil || string(cached) != text {
						t.Fatal("cached decrypt mismatch")
					}
					if withOPK {
						plain, failedKey, err := Open(ctx, receiver, signed, nil, parsed)
						if err == nil || plain != nil || failedKey != [32]byte{} {
							t.Fatal("missing OPK must fail without output")
						}
					} else if !strings.Contains(content, `"one_time_prekey_id":null`) {
						t.Fatal("3DH must encode explicit null")
					}
					second, secondKey, err := Seal(ctx, sender, bundle, []byte(text))
					if err != nil || second.EphemeralKey == envelope.EphemeralKey || second.Nonce == envelope.Nonce || secondKey == key {
						t.Fatal("new seal must use fresh crypto")
					}
				}
			})
		}
	}
}

func TestAADByteLayout(t *testing.T) {
	// Golden independent concatenation: public33, domain+NUL, four UUID16,
	// EK33 and two big-endian uint64; no server seq or timestamps.
	public := [32]byte(unhex(t, "8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a"))
	id := int64(0x0102030405060708)
	envelope := Envelope{EphemeralKey: b64(public[:]), SignedPrekeyID: id, OneTimePrekeyID: &id}
	prefix := "05" + hex.EncodeToString(public[:])
	want := prefix + prefix + "4d696e692d4865726d65732f653265655f763100" +
		"11111111111141118111111111111111" + "22222222222242228222222222222222" +
		"aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa" + "bbbbbbbbbbbb4bbb8bbbbbbbbbbbbbbb" + prefix +
		"0102030405060708" + "0102030405060708"
	aad, err := makeAAD(testContext(), public, public, envelope)
	if err != nil || len(aad) != 199 || hex.EncodeToString(aad) != want {
		t.Fatal("AAD golden mismatch")
	}
	envelope.OneTimePrekeyID = nil
	aad, err = makeAAD(testContext(), public, public, envelope)
	if err != nil || !bytes.Equal(aad[len(aad)-8:], make([]byte, 8)) {
		t.Fatal("absent OPK must bind zero uint64")
	}
}

func TestSignatureAndBundleValidation(t *testing.T) {
	ik, spk, opk := testPair(t), testPair(t), testPair(t)
	bundle := testBundle(t, bobID, ik, spk, &opk)
	if VerifyBundle(bundle) != nil {
		t.Fatal("valid signature rejected")
	}
	sig, _ := base64.StdEncoding.DecodeString(bundle.SignedPrekey.Signature)
	if ecc.VerifySignature(ecc.NewDjbECPublicKey(ik.Public), spk.Public[:], [64]byte(sig)) {
		t.Fatal("signature must bind type prefix")
	}
	for name, mutate := range map[string]func(*Bundle){
		"signature":       func(b *Bundle) { bad := append([]byte{}, sig...); bad[0] ^= 1; b.SignedPrekey.Signature = b64(bad) },
		"identity":        func(b *Bundle) { key := testPair(t); b.IdentityPublicKey = b64(key.Public[:]) },
		"signed":          func(b *Bundle) { key := testPair(t); b.SignedPrekey.PublicKey = b64(key.Public[:]) },
		"short signature": func(b *Bundle) { b.SignedPrekey.Signature = b64(sig[:63]) },
		"bad user":        func(b *Bundle) { b.UserID = strings.ToUpper(bobID) },
		"zero SPK ID":     func(b *Bundle) { b.SignedPrekey.KeyID = 0 },
		"same IDs":        func(b *Bundle) { b.OneTimePrekey = &PublicPrekey{KeyID: 1, PublicKey: b.OneTimePrekey.PublicKey} },
		"low OPK":         func(b *Bundle) { b.OneTimePrekey = &PublicPrekey{KeyID: 2, PublicKey: b64(make([]byte, 32))} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := bundle
			mutate(&bad)
			if !errors.Is(VerifyBundle(bad), ErrInvalidBundle) {
				t.Fatal("invalid bundle accepted")
			}
			envelope, key, err := Seal(testContext(), testPair(t), bad, []byte("x"))
			if err == nil || envelope.Version != 0 || key != [32]byte{} {
				t.Fatal("seal must reject bundle without output")
			}
		})
	}
	if _, err := SignPrekey(ik.Private, [32]byte{}); err == nil {
		t.Fatal("signed low-order key accepted")
	}
}

func TestConcurrentSignAndVerify(t *testing.T) {
	ik, spk := testPair(t), testPair(t)
	errors := make(chan bool, 16)
	for range 16 {
		go func() {
			signature, err := SignPrekey(ik.Private, spk.Public)
			bundle := Bundle{UserID: bobID, IdentityPublicKey: b64(ik.Public[:]), SignedPrekey: SignedPrekey{KeyID: 1, PublicKey: b64(spk.Public[:]), Signature: b64(signature[:])}}
			errors <- err != nil || VerifyBundle(bundle) != nil
		}()
	}
	for range 16 {
		if <-errors {
			t.Fatal("concurrent signing/verification failed")
		}
	}
}

func TestTamperingFailsWithoutOutput(t *testing.T) {
	sender, recipient, signed, opk := testPair(t), testPair(t), testPair(t), testPair(t)
	ctx := testContext()
	envelope, key, err := Seal(ctx, sender, testBundle(t, bobID, recipient, signed, &opk), []byte("bí mật"))
	if err != nil {
		t.Fatal("seal failed")
	}
	flip := func(encoded string) string {
		raw, _ := base64.StdEncoding.DecodeString(encoded)
		raw[0] ^= 1
		return b64(raw)
	}
	for name, mutate := range map[string]func(*MessageContext, *Envelope){
		"thread":           func(c *MessageContext, _ *Envelope) { c.ThreadID = aliceID },
		"message":          func(c *MessageContext, _ *Envelope) { c.MessageID = aliceID },
		"sender":           func(c *MessageContext, _ *Envelope) { c.SenderID = c.RecipientID },
		"recipient":        func(c *MessageContext, e *Envelope) { c.RecipientID = aliceID; e.RecipientID = aliceID },
		"recipient header": func(_ *MessageContext, e *Envelope) { e.RecipientID = aliceID },
		"IK":               func(_ *MessageContext, e *Envelope) { pair := testPair(t); e.SenderIdentityKey = b64(pair.Public[:]) },
		"EK":               func(_ *MessageContext, e *Envelope) { pair := testPair(t); e.EphemeralKey = b64(pair.Public[:]) },
		"SPK ID":           func(_ *MessageContext, e *Envelope) { e.SignedPrekeyID = 3 },
		"OPK ID":           func(_ *MessageContext, e *Envelope) { id := int64(3); e.OneTimePrekeyID = &id },
		"remove OPK":       func(_ *MessageContext, e *Envelope) { e.OneTimePrekeyID = nil },
		"nonce":            func(_ *MessageContext, e *Envelope) { e.Nonce = flip(e.Nonce) },
		"ciphertext":       func(_ *MessageContext, e *Envelope) { e.Ciphertext = flip(e.Ciphertext) },
	} {
		t.Run(name, func(t *testing.T) {
			badContext, badEnvelope := ctx, envelope
			mutate(&badContext, &badEnvelope)
			plain, failedKey, err := Open(badContext, recipient, signed, &opk, badEnvelope)
			if err == nil || plain != nil || failedKey != [32]byte{} {
				t.Fatal("tampered open returned output")
			}
			if plain, err := DecryptWithKey(badContext, sender.Public, recipient.Public, key, badEnvelope); err == nil || plain != nil {
				t.Fatal("tampered cached decrypt returned output")
			}
		})
	}
	wrongPair := recipient
	wrongPair.Public = sender.Public
	if plain, _, err := Open(ctx, wrongPair, signed, &opk, envelope); err == nil || plain != nil {
		t.Fatal("mismatched keypair accepted")
	}
	wrongKey := key
	wrongKey[0] ^= 1
	if plain, err := DecryptWithKey(ctx, sender.Public, recipient.Public, wrongKey, envelope); err == nil || plain != nil {
		t.Fatal("wrong cached key accepted")
	}
	if plain, _, err := Open(ctx, recipient, testPair(t), &opk, envelope); err == nil || plain != nil {
		t.Fatal("wrong private SPK accepted")
	}
	if plain, _, err := Open(ctx, recipient, signed, func() *KeyPair { p := testPair(t); return &p }(), envelope); err == nil || plain != nil {
		t.Fatal("wrong private OPK accepted")
	}
}

func TestLowOrderAndMalformedEnvelope(t *testing.T) {
	for _, encoded := range []string{
		strings.Repeat("00", 32), "01" + strings.Repeat("00", 31),
		"ec" + strings.Repeat("ff", 30) + "7f", "ed" + strings.Repeat("ff", 30) + "7f",
		"ee" + strings.Repeat("ff", 30) + "7f", "00" + strings.Repeat("00", 30) + "80",
		"e0eb7a7c3b41b8ae1656e3faf19fc46ada098deb9c32b1fd866205165f49b800",
		"5f9c95bca3508c24b1d0b1559c83ef5b04445cc4581c8e86d8224eddd09f1157",
	} {
		if ValidatePublicKey([32]byte(unhex(t, encoded))) == nil {
			t.Fatal("low-order public accepted")
		}
	}
	sender, recipient, signed := testPair(t), testPair(t), testPair(t)
	envelope, _, err := Seal(testContext(), sender, testBundle(t, bobID, recipient, signed, nil), []byte("x"))
	if err != nil {
		t.Fatal("seal failed")
	}
	content, _ := EncodeEnvelope(envelope)
	var fields map[string]any
	json.Unmarshal([]byte(content), &fields)
	for _, name := range []string{"version", "recipient_id", "sender_identity_key", "ephemeral_key", "signed_prekey_id", "one_time_prekey_id", "nonce", "ciphertext"} {
		copyFields := make(map[string]any)
		for k, v := range fields {
			copyFields[k] = v
		}
		delete(copyFields, name)
		raw, _ := json.Marshal(copyFields)
		if _, err := ParseEnvelope(string(raw)); err == nil {
			t.Fatal("missing envelope field accepted")
		}
	}
	for name, value := range map[string]any{
		"version": 2, "recipient_id": strings.ToUpper(bobID), "sender_identity_key": b64(make([]byte, 32)),
		"ephemeral_key": "bad", "signed_prekey_id": 0, "one_time_prekey_id": 1,
		"nonce": b64(make([]byte, 11)), "ciphertext": b64(make([]byte, 16)),
	} {
		t.Run(name, func(t *testing.T) {
			copyFields := make(map[string]any)
			for k, v := range fields {
				copyFields[k] = v
			}
			copyFields[name] = value
			raw, _ := json.Marshal(copyFields)
			if _, err := ParseEnvelope(string(raw)); err == nil {
				t.Fatal("malformed envelope accepted")
			}
		})
	}
	for _, raw := range []string{
		"null", "[]", "{}", content + "{}", content[:len(content)-1] + `,"extra":1}`,
		content[:len(content)-1] + `,"version":1}`, strings.Replace(content, `"version"`, `"Version"`, 1),
		strings.Replace(content, `"version":1`, `"version":null`, 1),
		strings.Replace(content, `"signed_prekey_id":1`, `"signed_prekey_id":9223372036854775808`, 1),
		strings.Replace(content, envelope.SenderIdentityKey, strings.TrimRight(envelope.SenderIdentityKey, "="), 1),
		strings.Replace(content, envelope.SenderIdentityKey, envelope.SenderIdentityKey[:4]+`\n`+envelope.SenderIdentityKey[4:], 1),
		strings.Replace(content, `"ciphertext":"`+envelope.Ciphertext+`"`, `"ciphertext":"`+b64(make([]byte, 4017))+`"`, 1),
		content + strings.Repeat(" ", 8193),
	} {
		if _, err := ParseEnvelope(raw); err == nil {
			t.Fatal("strict parser accepted invalid input")
		}
	}
	for _, invalid := range []string{b64(make([]byte, 31)), b64(make([]byte, 33)), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB=", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\r\n"} {
		if _, err := decode64(invalid, 32); err == nil {
			t.Fatal("noncanonical base64 accepted")
		}
	}
	envelope.Version = 2
	if raw, err := EncodeEnvelope(envelope); err == nil || raw != "" {
		t.Fatal("invalid encode returned content")
	}
}

func TestPlaintextAndContextValidation(t *testing.T) {
	sender, recipient, signed := testPair(t), testPair(t), testPair(t)
	ctx := testContext()
	bundle := testBundle(t, bobID, recipient, signed, nil)
	for _, plaintext := range [][]byte{nil, []byte("\x00"), {0xff}, []byte(strings.Repeat("a", 1001)), []byte(strings.Repeat("😀", 1001))} {
		if _, _, err := Seal(ctx, sender, bundle, plaintext); err == nil {
			t.Fatal("invalid plaintext sealed")
		}
	}
	for _, badID := range []string{"", "00000000-0000-0000-0000-000000000000", strings.ToUpper(aliceID), "{" + aliceID + "}"} {
		bad := ctx
		bad.MessageID = badID
		if _, _, err := Seal(bad, sender, bundle, []byte("x")); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	bad := ctx
	bad.RecipientID = aliceID
	if _, _, err := Seal(bad, sender, bundle, []byte("x")); err == nil {
		t.Fatal("bundle recipient mismatch accepted")
	}
	// Even correctly authenticated ciphertext must not surface invalid text.
	envelope, key, _ := Seal(ctx, sender, bundle, []byte("x"))
	aad, _ := makeAAD(ctx, sender.Public, recipient.Public, envelope)
	aead, _ := newGCM(key)
	nonce, _ := base64.StdEncoding.DecodeString(envelope.Nonce)
	for _, text := range [][]byte{{0xff}, []byte("a\x00"), []byte(strings.Repeat("a", 1001))} {
		bad := envelope
		bad.Ciphertext = b64(aead.Seal(nil, nonce, text, aad))
		plain, failedKey, err := Open(ctx, recipient, signed, nil, bad)
		if err == nil || plain != nil || failedKey != [32]byte{} {
			t.Fatal("authenticated invalid text returned")
		}
		if plain, err := DecryptWithKey(ctx, sender.Public, recipient.Public, key, bad); err == nil || plain != nil {
			t.Fatal("cached invalid text returned")
		}
	}
}
