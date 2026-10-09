package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/service"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

func epochRequest(t *testing.T) dto.CreateEpochRequest {
	t.Helper()
	sender, err := e2ee.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	public := fixture(t)
	bundle := e2ee.Bundle{UserID: peerUUID, IdentityPublicKey: public.IdentityPublicKey, SignedPrekey: public.SignedPrekey, OneTimePrekey: &public.OneTimePrekeys[0]}
	header, _, err := e2ee.CreateEpoch(e2ee.EpochContext{ThreadID: threadUUID, EpochID: epochUUID, SenderID: actorUUID, RecipientID: peerUUID}, sender, bundle)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := e2ee.EncodeEpochHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	backup := e2ee.EncryptedRecord{Version: 1, Nonce: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 12)), Ciphertext: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))}
	return dto.CreateEpochRequest{EpochID: epochUUID, Bootstrap: bootstrap, KeyBackup: backup}
}

func TestCreateEpochPreservesRetryAndConcurrentWinner(t *testing.T) {
	request := epochRequest(t)
	ctx := context.Background()
	want := dto.EpochResponse{EpochID: epochUUID, ThreadID: threadUUID, SenderID: actorUUID, RecipientID: peerUUID, Bootstrap: request.Bootstrap, KeyBackup: &request.KeyBackup}
	for _, created := range []bool{true, false} {
		calls := 0
		repo := &fakeRepository{create: func(got context.Context, actor int64, actorID, thread string, body dto.CreateEpochRequest) (dto.EpochResponse, bool, error) {
			calls++
			if got != ctx || actor != 101 || actorID != actorUUID || thread != threadUUID || !reflect.DeepEqual(body, request) {
				t.Fatal("authenticated actor or exact retry proposal changed")
			}
			return want, created, nil
		}}
		users := fakeUsers{get: func(context.Context, string) (usermodel.User, error) { return usermodel.User{ID: 101}, nil }}
		got, gotCreated, err := service.New(repo, users).CreateEpoch(ctx, actorUUID, threadUUID, request)
		if err != nil || gotCreated != created || !reflect.DeepEqual(got, want) || calls != 1 {
			t.Fatal("created/retry result changed")
		}
	}
	winner := want
	winner.EpochID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	conflict := &dto.EpochConflictError{Epoch: winner}
	repo := &fakeRepository{create: func(context.Context, int64, string, string, dto.CreateEpochRequest) (dto.EpochResponse, bool, error) {
		return want, true, conflict
	}}
	users := fakeUsers{get: func(context.Context, string) (usermodel.User, error) { return usermodel.User{ID: 101}, nil }}
	got, created, err := service.New(repo, users).CreateEpoch(ctx, actorUUID, threadUUID, request)
	var gotConflict *dto.EpochConflictError
	if got != (dto.EpochResponse{}) || created || !errors.Is(err, model.ErrEpochConflict) || !errors.As(err, &gotConflict) || !reflect.DeepEqual(gotConflict.Epoch, winner) {
		t.Fatal("conflict lost the committed winner or exposed the losing proposal")
	}
}

func TestEpochValidationRejectsMismatchedContextBeforePersistence(t *testing.T) {
	valid := epochRequest(t)
	for _, test := range []struct {
		name    string
		actor   string
		thread  string
		request dto.CreateEpochRequest
		want    error
	}{
		{name: "invalid actor", actor: "invalid", thread: threadUUID, request: valid, want: usermodel.ErrInvalidUserID},
		{name: "zero thread", actor: actorUUID, thread: "00000000-0000-0000-0000-000000000000", request: valid, want: model.ErrInvalidEpoch},
		{name: "different authenticated sender", actor: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", thread: threadUUID, request: valid, want: model.ErrInvalidEpoch},
		{name: "different route thread", actor: actorUUID, thread: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", request: valid, want: model.ErrInvalidEpoch},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := service.New(&fakeRepository{}, fakeUsers{}).CreateEpoch(context.Background(), test.actor, test.thread, test.request)
			if !errors.Is(err, test.want) {
				t.Fatal("invalid context reached user lookup or persistence")
			}
		})
	}
	for _, mutate := range []func(*dto.CreateEpochRequest){
		func(r *dto.CreateEpochRequest) { r.EpochID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb" },
		func(r *dto.CreateEpochRequest) { r.KeyBackup = e2ee.EncryptedRecord{} },
		func(r *dto.CreateEpochRequest) { r.Bootstrap = "not a bootstrap" },
	} {
		request := valid
		mutate(&request)
		_, _, err := service.New(&fakeRepository{}, fakeUsers{}).CreateEpoch(context.Background(), actorUUID, threadUUID, request)
		if !errors.Is(err, model.ErrInvalidEpoch) {
			t.Fatal("invalid proposal reached user lookup or persistence")
		}
	}
}

func TestRecoveryReturnsOnlyCommittedAccountAndBackup(t *testing.T) {
	ctx := context.Background()
	request := epochRequest(t)
	want := dto.AccountResponse{UserID: actorUUID, Username: "alice", AccountVault: request.KeyBackup}
	repo := &fakeRepository{
		account: func(got context.Context, actor int64) (dto.AccountResponse, error) {
			if got != ctx || actor != 101 {
				t.Fatal("account was not scoped to the authenticated user")
			}
			return want, nil
		},
		backup: func(got context.Context, actor int64, thread, epoch string, record e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error) {
			if got != ctx || actor != 101 || thread != threadUUID || epoch != epochUUID || record != request.KeyBackup {
				t.Fatal("backup context/owner changed")
			}
			return record, nil
		},
	}
	users := fakeUsers{get: func(context.Context, string) (usermodel.User, error) { return usermodel.User{ID: 101}, nil }}
	svc := service.New(repo, users)
	account, err := svc.Account(ctx, actorUUID)
	if err != nil || !reflect.DeepEqual(account, want) {
		t.Fatal("recovery account changed")
	}
	backup, err := svc.Backup(ctx, actorUUID, threadUUID, epochUUID, request.KeyBackup)
	if err != nil || backup != request.KeyBackup {
		t.Fatal("committed backup changed")
	}
	failure := errors.New("commit failed")
	repo.backup = func(context.Context, int64, string, string, e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error) {
		return request.KeyBackup, failure
	}
	backup, err = svc.Backup(ctx, actorUUID, threadUUID, epochUUID, request.KeyBackup)
	if !errors.Is(err, failure) || backup != (e2ee.EncryptedRecord{}) {
		t.Fatal("failed backup exposed uncommitted data")
	}
}
