package dto

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/module/message/model"
)

type CreateMessageRequest struct {
	SenderID   string `json:"sender_id"`
	ReceiverID string `json:"receiver_id"`
	Content    string `json:"content"`
}

type MessageResponse struct {
	ID         string    `json:"id"`
	SenderID   string    `json:"sender_id"`
	ReceiverID string    `json:"receiver_id"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

func ToMessageResponse(message model.Message) MessageResponse {
	return MessageResponse{
		ID:         message.ExternalID,
		SenderID:   message.SenderExternalID,
		ReceiverID: message.ReceiverExternalID,
		Content:    message.Content,
		CreatedAt:  message.CreatedAt,
	}
}

func ToMessageResponses(messages []model.Message) []MessageResponse {
	responses := make([]MessageResponse, len(messages))
	for i, message := range messages {
		responses[i] = ToMessageResponse(message)
	}

	return responses
}
