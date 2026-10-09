package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/service"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

const actorUUID = "f24d6027-27e9-4f2a-94ec-67b50d30a9cb"
const peerUUID = "9d27041e-3c5b-48cc-8c07-e942f7ee6bbb"
const threadUUID = "06c1fdaa-431d-4cab-9749-cd61f4b8b120"
const epochUUID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"

type fakeRepository struct {
	claim   func(context.Context, int64, int64, string) (e2ee.Bundle, error)
	account func(context.Context, int64) (dto.AccountResponse, error)
	epochs  func(context.Context, int64, string) (dto.EpochPageResponse, error)
	create  func(context.Context, int64, string, string, dto.CreateEpochRequest) (dto.EpochResponse, bool, error)
	backup  func(context.Context, int64, string, string, e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error)
}

func (r *fakeRepository) Claim(ctx context.Context, actor, peer int64, thread string) (e2ee.Bundle, error) {
	return r.claim(ctx, actor, peer, thread)
}
func (r *fakeRepository) Account(ctx context.Context, user int64) (dto.AccountResponse, error) {
	return r.account(ctx, user)
}
func (r *fakeRepository) Epochs(ctx context.Context, user int64, thread string) (dto.EpochPageResponse, error) {
	return r.epochs(ctx, user, thread)
}
func (r *fakeRepository) CreateEpoch(ctx context.Context, actor int64, actorID, thread string, request dto.CreateEpochRequest) (dto.EpochResponse, bool, error) {
	return r.create(ctx, actor, actorID, thread, request)
}
func (r *fakeRepository) Backup(ctx context.Context, actor int64, thread, epoch string, backup e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error) {
	return r.backup(ctx, actor, thread, epoch, backup)
}

type fakeUsers struct {
	get func(context.Context, string) (usermodel.User, error)
}

func (u fakeUsers) GetByExternalID(ctx context.Context, id string) (usermodel.User, error) {
	if u.get == nil {
		panic("unexpected user lookup")
	}
	return u.get(ctx, id)
}

func fixture(t *testing.T) e2ee.UploadRequest {
	t.Helper()
	ik, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal("generate identity fixture failed")
	}
	spk, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal("generate signed fixture failed")
	}
	opk, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal("generate OPK fixture failed")
	}
	sig, err := e2ee.SignPrekey(ik.Private, spk.Public)
	if err != nil {
		t.Fatal("sign fixture failed")
	}
	return e2ee.UploadRequest{
		IdentityPublicKey: base64.StdEncoding.EncodeToString(ik.Public[:]),
		SignedPrekey:      e2ee.SignedPrekey{KeyID: 1, PublicKey: base64.StdEncoding.EncodeToString(spk.Public[:]), Signature: base64.StdEncoding.EncodeToString(sig[:])},
		OneTimePrekeys:    []e2ee.PublicPrekey{{KeyID: 2, PublicKey: base64.StdEncoding.EncodeToString(opk.Public[:])}},
	}
}

func TestClaimValidationAndSelfClaim(t *testing.T) {
	tests := []struct {
		actor, peer, thread string
		want                error
	}{
		{"", peerUUID, threadUUID, usermodel.ErrInvalidUserID},
		{strings.ToUpper(actorUUID), peerUUID, threadUUID, usermodel.ErrInvalidUserID},
		{actorUUID, "invalid", threadUUID, model.ErrInvalidClaim},
		{actorUUID, "00000000-0000-0000-0000-000000000000", threadUUID, model.ErrInvalidClaim},
		{actorUUID, strings.ToUpper(peerUUID), threadUUID, model.ErrInvalidClaim},
		{actorUUID, peerUUID, "invalid", model.ErrInvalidClaim},
		{actorUUID, peerUUID, "00000000-0000-0000-0000-000000000000", model.ErrInvalidClaim},
		{actorUUID, peerUUID, strings.ToUpper(threadUUID), model.ErrInvalidClaim},
		{actorUUID, actorUUID, threadUUID, model.ErrForbidden},
	}
	for _, tt := range tests {
		bundle, err := service.New(&fakeRepository{}, fakeUsers{}).Claim(context.Background(), tt.actor, tt.peer, e2ee.ClaimRequest{ThreadID: tt.thread})
		if !errors.Is(err, tt.want) || !reflect.DeepEqual(bundle, e2ee.Bundle{}) {
			t.Fatal("invalid claim reached persistence or exposed a result")
		}
	}
	users := fakeUsers{get: func(context.Context, string) (usermodel.User, error) { return usermodel.User{ID: 101}, nil }}
	_, err := service.New(&fakeRepository{}, users).Claim(context.Background(), actorUUID, peerUUID, e2ee.ClaimRequest{ThreadID: threadUUID})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatal("self claim by internal ID accepted")
	}
}

func TestClaimPassesContextAndRepositoryBundle(t *testing.T) {
	request := fixture(t)
	ctx := context.Background()
	for _, withOPK := range []bool{false, true} {
		want := e2ee.Bundle{UserID: peerUUID, IdentityPublicKey: request.IdentityPublicKey, SignedPrekey: request.SignedPrekey}
		if withOPK {
			want.OneTimePrekey = &request.OneTimePrekeys[0]
		}
		lookups, claims := 0, 0
		users := fakeUsers{get: func(got context.Context, id string) (usermodel.User, error) {
			lookups++
			if got != ctx {
				t.Fatal("claim lookup context lost")
			}
			switch id {
			case actorUUID:
				return usermodel.User{ID: 101}, nil
			case peerUUID:
				return usermodel.User{ID: 202}, nil
			default:
				t.Fatal("unexpected claim user")
				return usermodel.User{}, nil
			}
		}}
		repo := &fakeRepository{claim: func(got context.Context, actor, peer int64, thread string) (e2ee.Bundle, error) {
			claims++
			if got != ctx || actor != 101 || peer != 202 || thread != threadUUID {
				t.Fatal("claim roles or context changed")
			}
			return want, nil
		}}
		got, err := service.New(repo, users).Claim(ctx, actorUUID, peerUUID, e2ee.ClaimRequest{ThreadID: threadUUID})
		if err != nil || !reflect.DeepEqual(got, want) || lookups != 2 || claims != 1 {
			t.Fatal("claim result or lookups differ")
		}
		if !withOPK {
			body, err := json.Marshal(got)
			if err != nil || !strings.Contains(string(body), `"one_time_prekey":null`) {
				t.Fatal("empty OPK must be explicit JSON null")
			}
		}
	}
}

func TestClaimAccessAndRepositoryErrors(t *testing.T) {
	for _, want := range []error{model.ErrForbidden, model.ErrNotDirectThread, model.ErrBundleNotFound, threadmodel.ErrThreadNotFound, usermodel.ErrUserNotFound, errors.New("commit failure")} {
		users := fakeUsers{get: func(_ context.Context, id string) (usermodel.User, error) {
			if id == actorUUID {
				return usermodel.User{ID: 101}, nil
			}
			return usermodel.User{ID: 202}, nil
		}}
		repo := &fakeRepository{claim: func(context.Context, int64, int64, string) (e2ee.Bundle, error) {
			return e2ee.Bundle{UserID: peerUUID}, want
		}}
		got, err := service.New(repo, users).Claim(context.Background(), actorUUID, peerUUID, e2ee.ClaimRequest{ThreadID: threadUUID})
		if !errors.Is(err, want) || !reflect.DeepEqual(got, e2ee.Bundle{}) {
			t.Fatal("claim error lost or uncommitted result exposed")
		}
	}
	for _, missing := range []string{actorUUID, peerUUID} {
		users := fakeUsers{get: func(_ context.Context, id string) (usermodel.User, error) {
			if id == missing {
				return usermodel.User{}, usermodel.ErrUserNotFound
			}
			return usermodel.User{ID: 101}, nil
		}}
		_, err := service.New(&fakeRepository{}, users).Claim(context.Background(), actorUUID, peerUUID, e2ee.ClaimRequest{ThreadID: threadUUID})
		want := usermodel.ErrUserNotFound
		if missing == actorUUID {
			want = usermodel.ErrInvalidUserID
		}
		if !errors.Is(err, want) {
			t.Fatal("missing claim user did not stop before repository")
		}
	}
}
