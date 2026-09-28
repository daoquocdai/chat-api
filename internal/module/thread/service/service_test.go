package service_test

import (
	"context"
	"errors"
	"testing"

	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	"github.com/daoquocdai/chat-api/internal/module/thread/service"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

const (
	actorExternalID  = "11111111-1111-4111-8111-111111111111"
	peerExternalID   = "22222222-2222-4222-8222-222222222222"
	threadExternalID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

type fakeThreadRepository struct {
	createOrGet func(context.Context, int64, int64) (threadmodel.Thread, bool, error)
	list        func(context.Context, int64) ([]threadmodel.Thread, error)
	markRead    func(context.Context, string, int64, int64) (int64, error)
	calls       int
}

func (r *fakeThreadRepository) CreateOrGetDirect(
	ctx context.Context,
	creatorID, peerID int64,
) (threadmodel.Thread, bool, error) {
	r.calls++
	return r.createOrGet(ctx, creatorID, peerID)
}

func (r *fakeThreadRepository) ListByUser(ctx context.Context, userID int64) ([]threadmodel.Thread, error) {
	r.calls++
	return r.list(ctx, userID)
}

func (r *fakeThreadRepository) MarkRead(
	ctx context.Context,
	threadID string,
	userID, lastReadSeq int64,
) (int64, error) {
	r.calls++
	return r.markRead(ctx, threadID, userID, lastReadSeq)
}

type fakeUserFinder struct {
	get   func(context.Context, string) (usermodel.User, error)
	calls int
}

func (f *fakeUserFinder) GetByExternalID(
	ctx context.Context,
	externalID string,
) (usermodel.User, error) {
	f.calls++
	return f.get(ctx, externalID)
}

func TestCreateOrGetDirect(t *testing.T) {
	databaseError := errors.New("database unavailable")

	tests := []struct {
		name             string
		peerID           string
		actorLookupError error
		peerLookupError  error
		peerInternalID   int64
		repositoryError  error
		wantError        error
		wantUserCalls    int
		wantRepoCalls    int
	}{
		{name: "creates direct thread", peerID: "  " + peerExternalID + " ", peerInternalID: 22, wantUserCalls: 2, wantRepoCalls: 1},
		{name: "peer ID required", peerID: " \t", wantError: threadmodel.ErrPeerIDRequired},
		{name: "same external user", peerID: actorExternalID, wantError: threadmodel.ErrSameUser},
		{name: "actor lookup fails", peerID: peerExternalID, actorLookupError: databaseError, wantError: databaseError, wantUserCalls: 1},
		{name: "peer lookup fails", peerID: peerExternalID, peerLookupError: usermodel.ErrUserNotFound, wantError: usermodel.ErrUserNotFound, wantUserCalls: 2},
		{name: "same internal user", peerID: peerExternalID, peerInternalID: 11, wantError: threadmodel.ErrSameUser, wantUserCalls: 2},
		{name: "repository error", peerID: peerExternalID, peerInternalID: 22, repositoryError: databaseError, wantError: databaseError, wantUserCalls: 2, wantRepoCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			users := &fakeUserFinder{get: func(gotCtx context.Context, externalID string) (usermodel.User, error) {
				if gotCtx != ctx {
					t.Fatal("context was not passed to user finder")
				}
				switch externalID {
				case actorExternalID:
					if tt.actorLookupError != nil {
						return usermodel.User{}, tt.actorLookupError
					}
					return usermodel.User{ID: 11, ExternalID: actorExternalID}, nil
				case peerExternalID:
					if tt.peerLookupError != nil {
						return usermodel.User{}, tt.peerLookupError
					}
					return usermodel.User{ID: tt.peerInternalID, ExternalID: peerExternalID}, nil
				default:
					t.Fatalf("unexpected external ID %q", externalID)
					return usermodel.User{}, nil
				}
			}}

			wantThread := threadmodel.Thread{ID: 7, ExternalID: threadExternalID, Kind: "direct"}
			repository := &fakeThreadRepository{createOrGet: func(
				gotCtx context.Context,
				creatorID, peerID int64,
			) (threadmodel.Thread, bool, error) {
				if gotCtx != ctx {
					t.Fatal("context was not passed to repository")
				}
				if creatorID != 11 || peerID != 22 {
					t.Fatalf("user IDs = (%d, %d), want (11, 22)", creatorID, peerID)
				}
				return wantThread, true, tt.repositoryError
			}}

			got, created, err := service.New(repository, users).CreateOrGetDirect(
				ctx,
				actorExternalID,
				tt.peerID,
			)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if users.calls != tt.wantUserCalls {
				t.Fatalf("user calls = %d, want %d", users.calls, tt.wantUserCalls)
			}
			if repository.calls != tt.wantRepoCalls {
				t.Fatalf("repository calls = %d, want %d", repository.calls, tt.wantRepoCalls)
			}
			if tt.wantError == nil && (got != wantThread || !created) {
				t.Fatalf("result = (%+v, %v), want (%+v, true)", got, created, wantThread)
			}
		})
	}
}

func TestMarkRead(t *testing.T) {
	tests := []struct {
		name          string
		threadID      string
		lastReadSeq   int64
		repositoryErr error
		want          int64
		wantError     error
		wantRepoCalls int
	}{
		{name: "advances marker", threadID: " " + threadExternalID + " ", lastReadSeq: 5, want: 5, wantRepoCalls: 1},
		{name: "empty thread ID", threadID: " ", wantError: threadmodel.ErrThreadIDRequired},
		{name: "negative marker", threadID: threadExternalID, lastReadSeq: -1, wantError: threadmodel.ErrInvalidReadSequence},
		{name: "participant error is preserved", threadID: threadExternalID, lastReadSeq: 1, repositoryErr: threadmodel.ErrNotParticipant, wantError: threadmodel.ErrNotParticipant, wantRepoCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			users := &fakeUserFinder{get: func(context.Context, string) (usermodel.User, error) {
				return usermodel.User{ID: 11, ExternalID: actorExternalID}, nil
			}}
			repository := &fakeThreadRepository{markRead: func(
				gotCtx context.Context,
				threadID string,
				userID, lastReadSeq int64,
			) (int64, error) {
				if gotCtx != ctx || threadID != threadExternalID || userID != 11 || lastReadSeq != tt.lastReadSeq {
					t.Fatalf("unexpected MarkRead arguments: %q %d %d", threadID, userID, lastReadSeq)
				}
				return tt.want, tt.repositoryErr
			}}

			got, err := service.New(repository, users).MarkRead(ctx, actorExternalID, tt.threadID, tt.lastReadSeq)
			if !errors.Is(err, tt.wantError) || got != tt.want {
				t.Fatalf("result = (%d, %v), want (%d, %v)", got, err, tt.want, tt.wantError)
			}
			if repository.calls != tt.wantRepoCalls {
				t.Fatalf("repository calls = %d, want %d", repository.calls, tt.wantRepoCalls)
			}
		})
	}
}
