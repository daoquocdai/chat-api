package dto

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/module/user/model"
)

type CreateUserRequest struct {
	Username string `json:"username"`
}

type UserResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

func ToUserResponse(user model.User) UserResponse {
	return UserResponse{
		ID:        user.ExternalID,
		Username:  user.Username,
		CreatedAt: user.CreatedAt,
	}
}
