package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/daoquocdai/chat-api/internal/module/user/dto"
	"github.com/daoquocdai/chat-api/internal/module/user/handler"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/daoquocdai/chat-api/internal/route"
)

type fakeService struct {
	create func(context.Context, string) (model.User, error)
	get    func(context.Context, string) (model.User, error)
}

func (s *fakeService) Create(
	ctx context.Context,
	username string,
) (model.User, error) {
	return s.create(ctx, username)
}

func (s *fakeService) GetByExternalID(
	ctx context.Context,
	externalID string,
) (model.User, error) {
	return s.get(ctx, externalID)
}

func TestUserHandler(t *testing.T) {
	user := model.User{
		ID:         12,
		ExternalID: "f24d6027-27e9-4f2a-94ec-67b50d30a9cb",
		Username:   "alice",
		CreatedAt:  time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC),
	}

	dbError := errors.New("database connection failed")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		serviceErr error
		wantStatus int
		wantError  string
		wantCall   string
		wantArg    string
	}{
		{
			name:       "create user",
			method:     http.MethodPost,
			path:       "/users",
			body:       `{"username":"alice"}`,
			wantStatus: http.StatusCreated,
			wantCall:   "create",
			wantArg:    "alice",
		},
		{
			name:       "invalid JSON",
			method:     http.MethodPost,
			path:       "/users",
			body:       `{"username":`,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid JSON body",
		},
		{
			name:       "invalid username",
			method:     http.MethodPost,
			path:       "/users",
			body:       `{"username":""}`,
			serviceErr: model.ErrInvalidUsername,
			wantStatus: http.StatusBadRequest,
			wantError:  model.ErrInvalidUsername.Error(),
			wantCall:   "create",
			wantArg:    "",
		},
		{
			name:       "duplicate username",
			method:     http.MethodPost,
			path:       "/users",
			body:       `{"username":"alice"}`,
			serviceErr: model.ErrUsernameTaken,
			wantStatus: http.StatusConflict,
			wantError:  "username already exists",
			wantCall:   "create",
			wantArg:    "alice",
		},
		{
			name:       "create database error",
			method:     http.MethodPost,
			path:       "/users",
			body:       `{"username":"alice"}`,
			serviceErr: dbError,
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal server error",
			wantCall:   "create",
			wantArg:    "alice",
		},
		{
			name:       "get user",
			method:     http.MethodGet,
			path:       "/users/" + user.ExternalID,
			wantStatus: http.StatusOK,
			wantCall:   "get",
			wantArg:    user.ExternalID,
		},
		{
			name:       "invalid user ID",
			method:     http.MethodGet,
			path:       "/users/invalid",
			serviceErr: model.ErrInvalidUserID,
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid user ID",
			wantCall:   "get",
			wantArg:    "invalid",
		},
		{
			name:       "user not found",
			method:     http.MethodGet,
			path:       "/users/" + user.ExternalID,
			serviceErr: model.ErrUserNotFound,
			wantStatus: http.StatusNotFound,
			wantError:  "user not found",
			wantCall:   "get",
			wantArg:    user.ExternalID,
		},
		{
			name:       "get database error",
			method:     http.MethodGet,
			path:       "/users/" + user.ExternalID,
			serviceErr: dbError,
			wantStatus: http.StatusInternalServerError,
			wantError:  "internal server error",
			wantCall:   "get",
			wantArg:    user.ExternalID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0

			checkCall := func(method, arg string) (model.User, error) {
				calls++

				if method != tt.wantCall {
					t.Fatalf(
						"service method = %q, want %q",
						method,
						tt.wantCall,
					)
				}

				if arg != tt.wantArg {
					t.Fatalf(
						"service argument = %q, want %q",
						arg,
						tt.wantArg,
					)
				}

				if tt.serviceErr != nil {
					return model.User{}, tt.serviceErr
				}

				return user, nil
			}

			svc := &fakeService{
				create: func(
					ctx context.Context,
					username string,
				) (model.User, error) {
					return checkCall("create", username)
				},
				get: func(
					ctx context.Context,
					externalID string,
				) (model.User, error) {
					return checkCall("get", externalID)
				},
			}

			userHandler := handler.New(svc)
			router := route.New(userHandler)

			request := httptest.NewRequest(
				tt.method,
				tt.path,
				strings.NewReader(tt.body),
			)
			request.Header.Set("Content-Type", "application/json")

			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			wantCalls := 0
			if tt.wantCall != "" {
				wantCalls = 1
			}

			if calls != wantCalls {
				t.Fatalf("service calls = %d, want %d", calls, wantCalls)
			}

			if response.Code != tt.wantStatus {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					response.Code,
					tt.wantStatus,
					response.Body.String(),
				)
			}

			contentType := response.Header().Get("Content-Type")
			if !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("unexpected Content-Type: %q", contentType)
			}

			if tt.wantError != "" {
				var body struct {
					Error string `json:"error"`
				}

				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error response: %v", err)
				}

				if body.Error != tt.wantError {
					t.Fatalf(
						"error message = %q, want %q",
						body.Error,
						tt.wantError,
					)
				}

				return
			}

			var body dto.UserResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode user response: %v", err)
			}

			if body.ID != user.ExternalID {
				t.Fatalf("ID = %q, want external ID %q", body.ID, user.ExternalID)
			}

			if body.Username != user.Username {
				t.Fatalf("username = %q, want %q", body.Username, user.Username)
			}

			if !body.CreatedAt.Equal(user.CreatedAt) {
				t.Fatalf("created_at = %v, want %v", body.CreatedAt, user.CreatedAt)
			}
		})
	}
}
