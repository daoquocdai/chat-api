package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/module/message/model"
	"github.com/daoquocdai/chat-api/internal/module/message/service"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

const (
	firstExternalID  = "11111111-1111-1111-1111-111111111111"
	secondExternalID = "22222222-2222-2222-2222-222222222222"
)

type fakeRepository struct {
	create           func(context.Context, int64, int64, string) (model.Message, error)
	listBetween      func(context.Context, int64, int64) ([]model.Message, error)
	createCalls      int
	listBetweenCalls int
}

func (r *fakeRepository) Create(
	ctx context.Context,
	senderID, receiverID int64,
	content string,
) (model.Message, error) {
	r.createCalls++
	return r.create(ctx, senderID, receiverID, content)
}

func (r *fakeRepository) ListBetween(
	ctx context.Context,
	userOneID, userTwoID int64,
) ([]model.Message, error) {
	r.listBetweenCalls++
	return r.listBetween(ctx, userOneID, userTwoID)
}

type fakeUserFinder struct {
	get   func(context.Context, string) (usermodel.User, error)
	calls int
}

func (f *fakeUserFinder) GetByExternalID(ctx context.Context, externalID string) (usermodel.User, error) {
	f.calls++
	return f.get(ctx, externalID)
}

func newFakeUserFinder(
	t *testing.T,
	wantContext context.Context,
	userErrors map[string]error,
	sameInternalUser bool,
) *fakeUserFinder {
	t.Helper()

	return &fakeUserFinder{get: func(ctx context.Context, externalID string) (usermodel.User, error) {
		if ctx != wantContext {
			t.Fatal("context was not passed to user service")
		}
		if err := userErrors[externalID]; err != nil {
			return usermodel.User{}, err
		}

		switch externalID {
		case firstExternalID:
			return usermodel.User{ID: 11, ExternalID: firstExternalID}, nil
		case secondExternalID:
			id := int64(22)
			if sameInternalUser {
				id = 11
			}
			return usermodel.User{ID: id, ExternalID: secondExternalID}, nil
		default:
			t.Fatalf("unexpected external ID: %q", externalID)
			return usermodel.User{}, nil
		}
	}}
}

func TestCreate(t *testing.T) {
	repositoryError := errors.New("database unavailable")

	tests := []struct {
		name             string
		senderID         string
		receiverID       string
		content          string
		userErrors       map[string]error
		sameInternalUser bool
		repositoryError  error
		wantError        error
		wantContent      string
		wantUserCalls    int
		wantRepoCalls    int
	}{
		{
			name:          "normalize valid message",
			senderID:      "  " + firstExternalID,
			receiverID:    secondExternalID + "\t",
			content:       "  Xin chào  ",
			wantContent:   "Xin chào",
			wantUserCalls: 2,
			wantRepoCalls: 1,
		},
		{
			name:       "missing IDs take priority over same ID and invalid content",
			senderID:   " ",
			receiverID: "\t",
			content:    " ",
			wantError:  model.ErrUserIDsRequired,
		},
		{
			name:       "same external ID takes priority over invalid content",
			senderID:   firstExternalID,
			receiverID: " " + firstExternalID + " ",
			content:    " ",
			wantError:  model.ErrSameUser,
		},
		{
			name:       "empty content",
			senderID:   firstExternalID,
			receiverID: secondExternalID,
			content:    " \t\n ",
			wantError:  model.ErrInvalidContent,
		},
		{
			name:       "content longer than 1000 unicode characters",
			senderID:   firstExternalID,
			receiverID: secondExternalID,
			content:    strings.Repeat("đ", 1001),
			wantError:  model.ErrInvalidContent,
		},
		{
			name:          "receiver not found",
			senderID:      firstExternalID,
			receiverID:    secondExternalID,
			content:       "hello",
			userErrors:    map[string]error{secondExternalID: usermodel.ErrUserNotFound},
			wantError:     usermodel.ErrUserNotFound,
			wantUserCalls: 2,
		},
		{
			name:             "different external IDs resolve to same user",
			senderID:         firstExternalID,
			receiverID:       secondExternalID,
			content:          "hello",
			sameInternalUser: true,
			wantError:        model.ErrSameUser,
			wantUserCalls:    2,
		},
		{
			name:            "repository error",
			senderID:        firstExternalID,
			receiverID:      secondExternalID,
			content:         "hello",
			repositoryError: repositoryError,
			wantError:       repositoryError,
			wantContent:     "hello",
			wantUserCalls:   2,
			wantRepoCalls:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			users := newFakeUserFinder(t, ctx, tt.userErrors, tt.sameInternalUser)

			savedMessage := model.Message{
				ID:                 1,
				ExternalID:         "33333333-3333-3333-3333-333333333333",
				SenderExternalID:   firstExternalID,
				ReceiverExternalID: secondExternalID,
				Content:            tt.wantContent,
			}

			repository := &fakeRepository{create: func(
				gotCtx context.Context,
				senderID, receiverID int64,
				content string,
			) (model.Message, error) {
				if gotCtx != ctx {
					t.Fatal("context was not passed to repository")
				}
				if senderID != 11 || receiverID != 22 {
					t.Fatalf("user IDs = (%d, %d), want (11, 22)", senderID, receiverID)
				}
				if content != tt.wantContent {
					t.Fatalf("content = %q, want %q", content, tt.wantContent)
				}
				if tt.repositoryError != nil {
					return model.Message{}, tt.repositoryError
				}
				return savedMessage, nil
			}}

			messageService := service.New(repository, users)
			got, err := messageService.Create(ctx, tt.senderID, tt.receiverID, tt.content)

			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if users.calls != tt.wantUserCalls {
				t.Fatalf("user service calls = %d, want %d", users.calls, tt.wantUserCalls)
			}
			if repository.createCalls != tt.wantRepoCalls {
				t.Fatalf("repository calls = %d, want %d", repository.createCalls, tt.wantRepoCalls)
			}

			if tt.wantError != nil {
				if got != (model.Message{}) {
					t.Fatalf("message = %+v, want empty message", got)
				}
				return
			}
			if got != savedMessage {
				t.Fatalf("message = %+v, want %+v", got, savedMessage)
			}
		})
	}
}

func TestListBetween(t *testing.T) {
	repositoryError := errors.New("database unavailable")
	wantMessages := []model.Message{{
		ID:                 9,
		ExternalID:         "33333333-3333-3333-3333-333333333333",
		SenderExternalID:   secondExternalID,
		ReceiverExternalID: firstExternalID,
		Content:            "hello",
	}}

	tests := []struct {
		name             string
		userID           string
		peerID           string
		userErrors       map[string]error
		sameInternalUser bool
		repositoryError  error
		wantError        error
		wantUserCalls    int
		wantRepoCalls    int
	}{
		{
			name:          "normalize IDs and list messages",
			userID:        " " + firstExternalID,
			peerID:        secondExternalID + " ",
			wantUserCalls: 2,
			wantRepoCalls: 1,
		},
		{
			name:      "missing peer ID",
			userID:    firstExternalID,
			peerID:    " ",
			wantError: model.ErrUserIDsRequired,
		},
		{
			name:      "same external ID",
			userID:    firstExternalID,
			peerID:    firstExternalID,
			wantError: model.ErrSameUser,
		},
		{
			name:          "peer not found",
			userID:        firstExternalID,
			peerID:        secondExternalID,
			userErrors:    map[string]error{secondExternalID: usermodel.ErrUserNotFound},
			wantError:     usermodel.ErrUserNotFound,
			wantUserCalls: 2,
		},
		{
			name:             "different external IDs resolve to same user",
			userID:           firstExternalID,
			peerID:           secondExternalID,
			sameInternalUser: true,
			wantError:        model.ErrSameUser,
			wantUserCalls:    2,
		},
		{
			name:            "repository error",
			userID:          firstExternalID,
			peerID:          secondExternalID,
			repositoryError: repositoryError,
			wantError:       repositoryError,
			wantUserCalls:   2,
			wantRepoCalls:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			users := newFakeUserFinder(t, ctx, tt.userErrors, tt.sameInternalUser)

			repository := &fakeRepository{listBetween: func(
				gotCtx context.Context,
				userOneID, userTwoID int64,
			) ([]model.Message, error) {
				if gotCtx != ctx {
					t.Fatal("context was not passed to repository")
				}
				if userOneID != 11 || userTwoID != 22 {
					t.Fatalf("user IDs = (%d, %d), want (11, 22)", userOneID, userTwoID)
				}
				if tt.repositoryError != nil {
					return nil, tt.repositoryError
				}
				return wantMessages, nil
			}}

			messageService := service.New(repository, users)
			got, err := messageService.ListBetween(ctx, tt.userID, tt.peerID)

			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if users.calls != tt.wantUserCalls {
				t.Fatalf("user service calls = %d, want %d", users.calls, tt.wantUserCalls)
			}
			if repository.listBetweenCalls != tt.wantRepoCalls {
				t.Fatalf("repository calls = %d, want %d", repository.listBetweenCalls, tt.wantRepoCalls)
			}

			if tt.wantError != nil {
				if got != nil {
					t.Fatalf("messages = %+v, want nil", got)
				}
				return
			}
			if len(got) != len(wantMessages) || got[0] != wantMessages[0] {
				t.Fatalf("messages = %+v, want %+v", got, wantMessages)
			}
		})
	}
}
