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

type fakeRepository struct {
	create      func(context.Context, int64, int64, string) (model.Message, error)
	listBetween func(context.Context, int64, int64) ([]model.Message, error)
}

func (r *fakeRepository) Create(
	ctx context.Context,
	senderID int64,
	receiverID int64,
	content string,
) (model.Message, error) {
	return r.create(ctx, senderID, receiverID, content)
}

func (r *fakeRepository) ListBetween(
	ctx context.Context,
	userOneID int64,
	userTwoID int64,
) ([]model.Message, error) {
	return r.listBetween(ctx, userOneID, userTwoID)
}

type fakeUserFinder struct {
	get func(context.Context, string) (usermodel.User, error)
}

func (f *fakeUserFinder) GetByExternalID(
	ctx context.Context,
	externalID string,
) (usermodel.User, error) {
	return f.get(ctx, externalID)
}

func TestCreate(t *testing.T) {
	const (
		senderExternalID   = "11111111-1111-1111-1111-111111111111"
		receiverExternalID = "22222222-2222-2222-2222-222222222222"
	)

	repositoryError := errors.New("database unavailable")

	tests := []struct {
		name            string
		senderID        string
		receiverID      string
		content         string
		userErrors      map[string]error
		repositoryError error
		wantError       error
		wantContent     string
		wantUserCalls   int
		wantRepoCalls   int
	}{
		{
			name:          "valid message",
			senderID:      senderExternalID,
			receiverID:    receiverExternalID,
			content:       "  Xin chào  ",
			wantContent:   "Xin chào",
			wantUserCalls: 2,
			wantRepoCalls: 1,
		},
		{
			name:       "empty content",
			senderID:   senderExternalID,
			receiverID: receiverExternalID,
			content:    " \t\n ",
			wantError:  model.ErrInvalidContent,
		},
		{
			name:       "content longer than 1000 unicode characters",
			senderID:   senderExternalID,
			receiverID: receiverExternalID,
			content:    strings.Repeat("đ", 1001),
			wantError:  model.ErrInvalidContent,
		},
		{
			name:       "send to self",
			senderID:   senderExternalID,
			receiverID: senderExternalID,
			content:    "hello",
			wantError:  model.ErrSameUser,
		},
		{
			name:       "receiver not found",
			senderID:   senderExternalID,
			receiverID: receiverExternalID,
			content:    "hello",
			userErrors: map[string]error{
				receiverExternalID: usermodel.ErrUserNotFound,
			},
			wantError:     usermodel.ErrUserNotFound,
			wantUserCalls: 2,
		},
		{
			name:            "repository error",
			senderID:        senderExternalID,
			receiverID:      receiverExternalID,
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
			userCalls := 0
			repositoryCalls := 0

			users := &fakeUserFinder{
				get: func(gotCtx context.Context, externalID string) (usermodel.User, error) {
					userCalls++
					if gotCtx != ctx {
						t.Fatal("context was not passed to user service")
					}

					if err := tt.userErrors[externalID]; err != nil {
						return usermodel.User{}, err
					}

					switch externalID {
					case senderExternalID:
						return usermodel.User{ID: 11, ExternalID: senderExternalID}, nil
					case receiverExternalID:
						return usermodel.User{ID: 22, ExternalID: receiverExternalID}, nil
					default:
						t.Fatalf("unexpected external ID: %q", externalID)
						return usermodel.User{}, nil
					}
				},
			}

			savedMessage := model.Message{
				ID:                 1,
				ExternalID:         "33333333-3333-3333-3333-333333333333",
				SenderExternalID:   senderExternalID,
				ReceiverExternalID: receiverExternalID,
				Content:            tt.wantContent,
			}

			repository := &fakeRepository{
				create: func(
					gotCtx context.Context,
					senderID int64,
					receiverID int64,
					content string,
				) (model.Message, error) {
					repositoryCalls++
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
				},
			}

			messageService := service.New(repository, users)
			got, err := messageService.Create(
				ctx,
				tt.senderID,
				tt.receiverID,
				tt.content,
			)

			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if userCalls != tt.wantUserCalls {
				t.Fatalf("user service calls = %d, want %d", userCalls, tt.wantUserCalls)
			}
			if repositoryCalls != tt.wantRepoCalls {
				t.Fatalf("repository calls = %d, want %d", repositoryCalls, tt.wantRepoCalls)
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
	const (
		userExternalID = "11111111-1111-1111-1111-111111111111"
		peerExternalID = "22222222-2222-2222-2222-222222222222"
	)

	ctx := context.Background()
	want := []model.Message{{
		ID:                 9,
		ExternalID:         "33333333-3333-3333-3333-333333333333",
		SenderExternalID:   peerExternalID,
		ReceiverExternalID: userExternalID,
		Content:            "hello",
	}}

	users := &fakeUserFinder{
		get: func(gotCtx context.Context, externalID string) (usermodel.User, error) {
			if gotCtx != ctx {
				t.Fatal("context was not passed to user service")
			}
			if externalID == userExternalID {
				return usermodel.User{ID: 11, ExternalID: userExternalID}, nil
			}
			if externalID == peerExternalID {
				return usermodel.User{ID: 22, ExternalID: peerExternalID}, nil
			}
			t.Fatalf("unexpected external ID: %q", externalID)
			return usermodel.User{}, nil
		},
	}

	repositoryCalls := 0
	repository := &fakeRepository{
		listBetween: func(
			gotCtx context.Context,
			userOneID int64,
			userTwoID int64,
		) ([]model.Message, error) {
			repositoryCalls++
			if gotCtx != ctx {
				t.Fatal("context was not passed to repository")
			}
			if userOneID != 11 || userTwoID != 22 {
				t.Fatalf("user IDs = (%d, %d), want (11, 22)", userOneID, userTwoID)
			}
			return want, nil
		},
	}

	messageService := service.New(repository, users)
	got, err := messageService.ListBetween(ctx, userExternalID, peerExternalID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repositoryCalls != 1 {
		t.Fatalf("repository calls = %d, want 1", repositoryCalls)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("messages = %+v, want %+v", got, want)
	}
}
