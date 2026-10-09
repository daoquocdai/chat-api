package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/daoquocdai/chat-api/internal/module/user/service"
	"golang.org/x/crypto/bcrypt"
)

func registrationFixture(t *testing.T) (string, e2ee.KDFProfile, e2ee.UploadRequest, e2ee.EncryptedRecord) {
	t.Helper()
	identity, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	signed, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := e2ee.SignPrekey(identity.Private, signed.Public)
	if err != nil {
		t.Fatal(err)
	}
	bundle := e2ee.UploadRequest{
		IdentityPublicKey: base64.StdEncoding.EncodeToString(identity.Public[:]),
		SignedPrekey: e2ee.SignedPrekey{
			KeyID: 1, PublicKey: base64.StdEncoding.EncodeToString(signed.Public[:]), Signature: base64.StdEncoding.EncodeToString(signature[:]),
		},
	}
	for id := int64(2); id <= 21; id++ {
		pair, err := e2ee.GenerateKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		bundle.OneTimePrekeys = append(bundle.OneTimePrekeys, e2ee.PublicPrekey{KeyID: id, PublicKey: base64.StdEncoding.EncodeToString(pair.Public[:])})
	}
	kdf := e2ee.KDFProfile{Version: 1, Algorithm: "argon2id", Salt: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 16)), MemoryKiB: 65536, Iterations: 3, Parallelism: 4}
	vault := e2ee.EncryptedRecord{Version: 1, Nonce: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 12)), Ciphertext: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))}
	credential := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{5}, 32))
	return credential, kdf, bundle, vault
}

func TestDerivedCredentialRegistrationAndLogin(t *testing.T) {
	credential, kdf, bundle, vault := registrationFixture(t)
	ctx := context.Background()
	want := model.User{ID: 7, ExternalID: "11111111-1111-4111-8111-111111111111", Username: "alice"}
	var stored model.AccountRegistration
	repo := &fakeRepository{
		create: func(gotCtx context.Context, registration model.AccountRegistration) (model.User, error) {
			if gotCtx != ctx {
				t.Fatal("registration context lost")
			}
			stored = registration
			return want, nil
		},
		credentials: func(gotCtx context.Context, username string) (model.Credentials, error) {
			if gotCtx != ctx || username != "alice" {
				t.Fatal("login context/username lost")
			}
			return model.Credentials{User: want, AuthCredentialHash: stored.AuthCredentialHash}, nil
		},
	}
	tokenCalls := 0
	svc := service.New(repo, fakeTokenCreator{create: func(userID string) (string, error) {
		if userID != want.ExternalID {
			t.Fatal("JWT subject is not the registered user")
		}
		tokenCalls++
		return "test-token", nil
	}})
	got, err := svc.Register(ctx, "  ALICE  ", credential, kdf, bundle, vault)
	if err != nil || got != want {
		t.Fatalf("registration user/error: %v %v", got, err)
	}
	if stored.Username != "alice" || stored.AuthCredentialHash == credential ||
		!reflect.DeepEqual(stored.KDF, kdf) || !reflect.DeepEqual(stored.PublicBundle, bundle) || stored.AccountVault != vault {
		t.Fatal("registration did not preserve recovery data or hash the credential")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.AuthCredentialHash), []byte(credential)); err != nil {
		t.Fatal("canonical Base64 credential string was not hashed")
	}
	decoded, _ := base64.StdEncoding.DecodeString(credential)
	if bcrypt.CompareHashAndPassword([]byte(stored.AuthCredentialHash), decoded) == nil {
		t.Fatal("decoded credential bytes unexpectedly authenticate")
	}
	login, err := svc.Login(ctx, " ALICE ", credential)
	if err != nil || login != (model.LoginResult{AccessToken: "test-token", UserID: want.ExternalID}) || tokenCalls != 1 {
		t.Fatalf("login result/error/calls: %v %v %d", login, err, tokenCalls)
	}
	wrong := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{6}, 32))
	if _, err := svc.Login(ctx, "alice", wrong); !errors.Is(err, model.ErrInvalidCredentials) || tokenCalls != 1 {
		t.Fatal("wrong credential obtained a token")
	}
}

func TestRegistrationRejectsInvalidRecoveryDataBeforeDatabase(t *testing.T) {
	credential, kdf, bundle, vault := registrationFixture(t)
	for _, test := range []struct {
		name       string
		credential string
		kdf        e2ee.KDFProfile
		bundle     e2ee.UploadRequest
		vault      e2ee.EncryptedRecord
		want       error
	}{
		{name: "raw password", credential: "my-long-password", kdf: kdf, bundle: bundle, vault: vault, want: model.ErrInvalidAuthCredential},
		{name: "noncanonical credential", credential: strings.TrimRight(credential, "="), kdf: kdf, bundle: bundle, vault: vault, want: model.ErrInvalidAuthCredential},
		{name: "missing KDF", credential: credential, bundle: bundle, vault: vault, want: model.ErrInvalidAccountData},
		{name: "missing public bundle", credential: credential, kdf: kdf, vault: vault, want: model.ErrInvalidAccountData},
		{name: "missing encrypted vault", credential: credential, kdf: kdf, bundle: bundle, want: model.ErrInvalidAccountData},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Empty callbacks deliberately fail the test if invalid input reaches the DB.
			svc := service.New(&fakeRepository{}, fakeTokenCreator{})
			_, err := svc.Register(context.Background(), "alice", test.credential, test.kdf, test.bundle, test.vault)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	for _, invalid := range []string{"my-long-password", strings.TrimRight(credential, "="), ""} {
		_, err := service.New(&fakeRepository{}, fakeTokenCreator{}).Login(context.Background(), "alice", invalid)
		if !errors.Is(err, model.ErrInvalidCredentials) {
			t.Fatal("invalid credential reached login DB lookup")
		}
	}
}

func TestAuthParamsNormalizesUsernameAndReturnsStoredProfile(t *testing.T) {
	_, kdf, _, _ := registrationFixture(t)
	want := model.AuthParams{Username: "alice", KDF: kdf}
	repo := &fakeRepository{params: func(_ context.Context, username string) (model.AuthParams, error) {
		if username != "alice" {
			t.Fatal("auth params username was not normalized")
		}
		return want, nil
	}}
	got, err := service.New(repo, fakeTokenCreator{}).AuthParams(context.Background(), " ALICE ")
	if err != nil || got != want {
		t.Fatalf("auth params/error = %v %v", got, err)
	}
}
