package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/daoquocdai/chat-api/internal/module/user/service"
)

// Repository giả: mỗi bài kiểm thử tự quyết định kết quả trả về.
type fakeRepository struct {
	create func(context.Context, string) (model.User, error)
	get    func(context.Context, string) (model.User, error)
}

func (r *fakeRepository) Create(
	ctx context.Context,
	username string,
) (model.User, error) {
	return r.create(ctx, username)
}

func (r *fakeRepository) GetByExternalID(
	ctx context.Context,
	externalID string,
) (model.User, error) {
	return r.get(ctx, externalID)
}

func TestCreate(t *testing.T) {
	dbError := errors.New("database unavailable")

	tests := []struct {
		name         string
		username     string
		wantUsername string
		repoError    error
		wantError    error
		wantCalls    int
	}{
		{
			name:         "normalize username",
			username:     " Alice ",
			wantUsername: "alice",
			wantCalls:    1,
		},
		{
			name:      "empty username",
			username:  "",
			wantError: model.ErrInvalidUsername,
		},
		{
			name:      "only whitespace",
			username:  " \t\n ",
			wantError: model.ErrInvalidUsername,
		},
		{
			name:         "50 unicode characters",
			username:     strings.Repeat("đ", 50),
			wantUsername: strings.Repeat("đ", 50),
			wantCalls:    1,
		},
		{
			name:      "51 unicode characters",
			username:  strings.Repeat("đ", 51),
			wantError: model.ErrInvalidUsername,
		},
		{
			name:      "username containing NUL",
			username:  "ali\x00ce",
			wantError: model.ErrInvalidUsername,
		},
		{
			name:         "duplicate username",
			username:     "alice",
			wantUsername: "alice",
			repoError:    model.ErrUsernameTaken,
			wantError:    model.ErrUsernameTaken,
			wantCalls:    1,
		},
		{
			name:         "database error",
			username:     "alice",
			wantUsername: "alice",
			repoError:    dbError,
			wantError:    dbError,
			wantCalls:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			calls := 0

			savedUser := model.User{
				ID:         1,
				ExternalID: "f24d6027-27e9-4f2a-94ec-67b50d30a9cb",
				Username:   tt.wantUsername,
			}

			repo := &fakeRepository{
				create: func(
					gotCtx context.Context,
					username string,
				) (model.User, error) {
					calls++

					if gotCtx != ctx {
						t.Fatal("context was not passed to repository")
					}

					if username != tt.wantUsername {
						t.Fatalf(
							"username = %q, want %q",
							username,
							tt.wantUsername,
						)
					}

					if tt.repoError != nil {
						return model.User{}, tt.repoError
					}

					return savedUser, nil
				},
			}

			svc := service.New(repo)
			got, err := svc.Create(ctx, tt.username)

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

			svc := service.New(repo)
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
