package dto

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/module/user/model"
)

type CreateUserRequest struct {
	Username string `json:"username"`
}

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type UserSummaryResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func ToUserResponse(user model.User) UserResponse {
	return UserResponse{
		ID:        user.ExternalID,
		Username:  user.Username,
		CreatedAt: user.CreatedAt,
	}
}

func ToUserSummaryResponses(users []model.User) []UserSummaryResponse {
	responses := make([]UserSummaryResponse, len(users))
	for i, user := range users {
		responses[i] = UserSummaryResponse{
			ID:       user.ExternalID,
			Username: user.Username,
		}
	}

	return responses
}
