package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/daoquocdai/chat-api/internal/module/user/service"
)

type fakeRepository struct {
	get  func(context.Context, string) (model.User, error)
	list func(context.Context) ([]model.User, error)
}

func (r *fakeRepository) CreateWithPassword(context.Context, string, string) (model.User, error) {
	panic("unexpected CreateWithPassword call")
}

func (r *fakeRepository) GetCredentialsByUsername(context.Context, string) (model.Credentials, error) {
	panic("unexpected GetCredentialsByUsername call")
}

type fakeTokenCreator struct{}

func (fakeTokenCreator) Create(string) (string, error) {
	panic("unexpected token creation")
}

func (r *fakeRepository) List(ctx context.Context) ([]model.User, error) {
	return r.list(ctx)
}

func (r *fakeRepository) GetByExternalID(ctx context.Context, externalID string) (model.User, error) {
	return r.get(ctx, externalID)
}

func TestGetByExternalID(t *testing.T) {
	dbError := errors.New("database unavailable")
	validID := "f24d6027-27e9-4f2a-94ec-67b50d30a9cb"

	tests := []struct {
		name       string
		externalID string
		repoError  error
		wantError  error
		wantCalls  int
	}{
		{
			name:       "found user",
			externalID: validID,
			wantCalls:  1,
		},
		{
			name:      "empty ID",
			wantError: model.ErrInvalidUserID,
		},
		{
			name:       "invalid ID reported by repository",
			externalID: "invalid",
			repoError:  model.ErrInvalidUserID,
			wantError:  model.ErrInvalidUserID,
			wantCalls:  1,
		},
		{
			name:       "user not found",
			externalID: validID,
			repoError:  model.ErrUserNotFound,
			wantError:  model.ErrUserNotFound,
			wantCalls:  1,
		},
		{
			name:       "database error",
			externalID: validID,
			repoError:  dbError,
			wantError:  dbError,
			wantCalls:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			calls := 0

			savedUser := model.User{
				ID:         1,
				ExternalID: validID,
				Username:   "alice",
			}

			repo := &fakeRepository{
				get: func(
					gotCtx context.Context,
					externalID string,
				) (model.User, error) {
					calls++

					if gotCtx != ctx {
						t.Fatal("context was not passed to repository")
					}

					if externalID != tt.externalID {
						t.Fatalf(
							"externalID = %q, want %q",
							externalID,
							tt.externalID,
						)
					}

					if tt.repoError != nil {
						return model.User{}, tt.repoError
					}

					return savedUser, nil
				},
			}

			svc := service.New(repo, fakeTokenCreator{})
			got, err := svc.GetByExternalID(ctx, tt.externalID)

			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}

			if calls != tt.wantCalls {
				t.Fatalf(
					"repository calls = %d, want %d",
					calls,
					tt.wantCalls,
				)
			}

			if tt.wantError != nil {
				if got != (model.User{}) {
					t.Fatalf("user = %+v, want empty user", got)
				}
				return
			}

			if got != savedUser {
				t.Fatalf("user = %+v, want %+v", got, savedUser)
			}
		})
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	want := []model.User{
		{ID: 1, ExternalID: "11111111-1111-1111-1111-111111111111", Username: "alice"},
		{ID: 2, ExternalID: "22222222-2222-2222-2222-222222222222", Username: "bob"},
	}

	repo := &fakeRepository{
		list: func(gotCtx context.Context) ([]model.User, error) {
			if gotCtx != ctx {
				t.Fatal("context was not passed to repository")
			}
			return want, nil
		},
	}

	svc := service.New(repo, fakeTokenCreator{})
	got, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("users length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("user %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
