package service_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/service"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

const actorUUID = "f24d6027-27e9-4f2a-94ec-67b50d30a9cb"
const peerUUID = "9d27041e-3c5b-48cc-8c07-e942f7ee6bbb"
const threadUUID = "06c1fdaa-431d-4cab-9749-cd61f4b8b120"

type fakeRepository struct {
	upload func(context.Context, int64, e2ee.UploadRequest) (e2ee.UploadResult, error)
	claim  func(context.Context, int64, int64, string) (e2ee.Bundle, error)
}

func (r *fakeRepository) Upload(ctx context.Context, id int64, request e2ee.UploadRequest) (e2ee.UploadResult, error) {
	if r.upload == nil {
		panic("unexpected upload")
	}
	return r.upload(ctx, id, request)
}
func (r *fakeRepository) Claim(ctx context.Context, actor, peer int64, thread string) (e2ee.Bundle, error) {
	if r.claim == nil {
		panic("unexpected claim")
	}
	return r.claim(ctx, actor, peer, thread)
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

func copyRequest(request e2ee.UploadRequest) e2ee.UploadRequest {
	request.OneTimePrekeys = append([]e2ee.PublicPrekey{}, request.OneTimePrekeys...)
	return request
}

func TestUploadValidationBeforeUserLookup(t *testing.T) {
	valid := fixture(t)
	other := fixture(t)
	zeroKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	one := make([]byte, 32)
	one[0] = 1
	oneKey := base64.StdEncoding.EncodeToString(one)
	tests := []struct {
		name   string
		mutate func(*e2ee.UploadRequest)
	}{
		{"identity base64", func(r *e2ee.UploadRequest) { r.IdentityPublicKey = "!" }},
		{"identity raw base64", func(r *e2ee.UploadRequest) { r.IdentityPublicKey = strings.TrimRight(r.IdentityPublicKey, "=") }},
		{"identity newline", func(r *e2ee.UploadRequest) { r.IdentityPublicKey += "\n" }},
		{"identity short", func(r *e2ee.UploadRequest) { r.IdentityPublicKey = base64.StdEncoding.EncodeToString(make([]byte, 31)) }},
		{"identity long", func(r *e2ee.UploadRequest) { r.IdentityPublicKey = base64.StdEncoding.EncodeToString(make([]byte, 33)) }},
		{"identity low order", func(r *e2ee.UploadRequest) { r.IdentityPublicKey = zeroKey }},
		{"signed base64", func(r *e2ee.UploadRequest) { r.SignedPrekey.PublicKey = "!" }},
		{"signed short", func(r *e2ee.UploadRequest) {
			r.SignedPrekey.PublicKey = base64.StdEncoding.EncodeToString(make([]byte, 31))
		}},
		{"signed low order", func(r *e2ee.UploadRequest) { r.SignedPrekey.PublicKey = oneKey }},
		{"wrong signed key", func(r *e2ee.UploadRequest) { r.SignedPrekey.PublicKey = other.SignedPrekey.PublicKey }},
		{"signature base64", func(r *e2ee.UploadRequest) { r.SignedPrekey.Signature = "!" }},
		{"signature short", func(r *e2ee.UploadRequest) {
			r.SignedPrekey.Signature = base64.StdEncoding.EncodeToString(make([]byte, 63))
		}},
		{"signature long", func(r *e2ee.UploadRequest) {
			r.SignedPrekey.Signature = base64.StdEncoding.EncodeToString(make([]byte, 65))
		}},
		{"signature wrong identity", func(r *e2ee.UploadRequest) { r.IdentityPublicKey = other.IdentityPublicKey }},
		{"signature tampered", func(r *e2ee.UploadRequest) {
			b, _ := base64.StdEncoding.DecodeString(r.SignedPrekey.Signature)
			b[0] ^= 1
			r.SignedPrekey.Signature = base64.StdEncoding.EncodeToString(b)
		}},
		{"zero signed ID", func(r *e2ee.UploadRequest) { r.SignedPrekey.KeyID = 0 }},
		{"negative signed ID", func(r *e2ee.UploadRequest) { r.SignedPrekey.KeyID = -1 }},
		{"OPK base64", func(r *e2ee.UploadRequest) { r.OneTimePrekeys[0].PublicKey = "!" }},
		{"OPK raw base64", func(r *e2ee.UploadRequest) {
			r.OneTimePrekeys[0].PublicKey = strings.TrimRight(r.OneTimePrekeys[0].PublicKey, "=")
		}},
		{"OPK newline", func(r *e2ee.UploadRequest) { r.OneTimePrekeys[0].PublicKey += "\r\n" }},
		{"OPK short", func(r *e2ee.UploadRequest) {
			r.OneTimePrekeys[0].PublicKey = base64.StdEncoding.EncodeToString(make([]byte, 31))
		}},
		{"OPK long", func(r *e2ee.UploadRequest) {
			r.OneTimePrekeys[0].PublicKey = base64.StdEncoding.EncodeToString(make([]byte, 33))
		}},
		{"OPK low order zero", func(r *e2ee.UploadRequest) { r.OneTimePrekeys[0].PublicKey = zeroKey }},
		{"OPK low order one", func(r *e2ee.UploadRequest) { r.OneTimePrekeys[0].PublicKey = oneKey }},
		{"zero OPK ID", func(r *e2ee.UploadRequest) { r.OneTimePrekeys[0].KeyID = 0 }},
		{"negative OPK ID", func(r *e2ee.UploadRequest) { r.OneTimePrekeys[0].KeyID = -1 }},
		{"OPK duplicates SPK ID", func(r *e2ee.UploadRequest) { r.OneTimePrekeys[0].KeyID = r.SignedPrekey.KeyID }},
		{"duplicate OPK IDs", func(r *e2ee.UploadRequest) { r.OneTimePrekeys = append(r.OneTimePrekeys, r.OneTimePrekeys[0]) }},
		{"101 OPKs", func(r *e2ee.UploadRequest) {
			key := r.OneTimePrekeys[0].PublicKey
			r.OneTimePrekeys = make([]e2ee.PublicPrekey, 101)
			for i := range r.OneTimePrekeys {
				r.OneTimePrekeys[i] = e2ee.PublicPrekey{KeyID: int64(i + 2), PublicKey: key}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := copyRequest(valid)
			tt.mutate(&request)
			result, err := service.New(&fakeRepository{}, fakeUsers{}).Upload(context.Background(), actorUUID, request)
			if !errors.Is(err, model.ErrInvalidPrekeys) || result != (e2ee.UploadResult{}) {
				t.Fatal("invalid upload reached persistence or exposed a result")
			}
		})
	}
}

func TestUploadPassesActorAndExactRetryOrRefill(t *testing.T) {
	valid := fixture(t)
	ctx := context.Background()
	for _, count := range []int{0, 1, 100} {
		t.Run(strconv.Itoa(count)+" OPKs", func(t *testing.T) {
			request := copyRequest(valid)
			request.OneTimePrekeys = make([]e2ee.PublicPrekey, count)
			for i := range request.OneTimePrekeys {
				request.OneTimePrekeys[i] = e2ee.PublicPrekey{KeyID: int64(102 - i), PublicKey: valid.OneTimePrekeys[0].PublicKey}
			}
			if count == 1 {
				request.OneTimePrekeys[0].KeyID = math.MaxInt64
			}
			want := e2ee.UploadResult{UserID: actorUUID, IdentityPublicKey: valid.IdentityPublicKey, SignedPrekeyID: 1, LastPrekeyID: 103, OneTimePrekeyCount: int64(count)}
			uploads, lookups := 0, 0
			repo := &fakeRepository{upload: func(got context.Context, id int64, body e2ee.UploadRequest) (e2ee.UploadResult, error) {
				uploads++
				if got != ctx || id != 101 || !reflect.DeepEqual(body, request) {
					t.Fatal("actor, context or exact upload changed")
				}
				return want, nil
			}}
			users := fakeUsers{get: func(got context.Context, externalID string) (usermodel.User, error) {
				lookups++
				if got != ctx || externalID != actorUUID {
					t.Fatal("upload owner was not the authenticated actor")
				}
				return usermodel.User{ID: 101, ExternalID: actorUUID}, nil
			}}
			svc := service.New(repo, users)
			for i := 0; i < 2; i++ {
				result, err := svc.Upload(ctx, actorUUID, request)
				if err != nil || result != want {
					t.Fatal("repository watermark/count were not preserved on upload/retry")
				}
			}
			if uploads != 2 || lookups != 2 {
				t.Fatal("unexpected upload/retry calls")
			}
		})
	}
}

func TestUploadErrorsDoNotExposeSuccess(t *testing.T) {
	request := fixture(t)
	for _, actor := range []string{"", "invalid", "00000000-0000-0000-0000-000000000000", strings.ToUpper(actorUUID), strings.ReplaceAll(actorUUID, "-", "")} {
		result, err := service.New(&fakeRepository{}, fakeUsers{}).Upload(context.Background(), actor, request)
		if !errors.Is(err, usermodel.ErrInvalidUserID) || result != (e2ee.UploadResult{}) {
			t.Fatal("invalid actor was accepted")
		}
	}
	for _, want := range []error{model.ErrKeyConflict, usermodel.ErrUserNotFound, errors.New("database failure")} {
		repo := &fakeRepository{upload: func(context.Context, int64, e2ee.UploadRequest) (e2ee.UploadResult, error) {
			return e2ee.UploadResult{LastPrekeyID: 999}, want
		}}
		users := fakeUsers{get: func(context.Context, string) (usermodel.User, error) { return usermodel.User{ID: 101}, nil }}
		result, err := service.New(repo, users).Upload(context.Background(), actorUUID, request)
		if !errors.Is(err, want) || result != (e2ee.UploadResult{}) {
			t.Fatal("upload error lost or success exposed")
		}
	}
	result, err := service.New(&fakeRepository{}, fakeUsers{get: func(context.Context, string) (usermodel.User, error) {
		return usermodel.User{}, usermodel.ErrUserNotFound
	}}).Upload(context.Background(), actorUUID, request)
	if !errors.Is(err, usermodel.ErrUserNotFound) || result != (e2ee.UploadResult{}) {
		t.Fatal("missing actor was not propagated")
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
	for _, want := range []error{model.ErrForbidden, model.ErrThreadMode, model.ErrBundleNotFound, threadmodel.ErrThreadNotFound, usermodel.ErrUserNotFound, errors.New("commit failure")} {
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
		if !errors.Is(err, usermodel.ErrUserNotFound) {
			t.Fatal("missing claim user did not stop before repository")
		}
	}
}
