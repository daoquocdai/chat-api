package dto

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/module/message/model"
)

type SendMessageRequest struct {
	ClientMessageID string `json:"client_msg_id"`
	Content         string `json:"content"`
}

type MessageResponse struct {
	ID              string    `json:"id"`
	ThreadID        string    `json:"thread_id"`
	SenderID        string    `json:"sender_id"`
	Seq             int64     `json:"seq"`
	ClientMessageID string    `json:"client_msg_id"`
	Kind            string    `json:"kind"`
	ContentFormat   string    `json:"content_format"`
	Content         string    `json:"content"`
	CreatedAt       time.Time `json:"created_at"`
}

func ToMessageResponse(message model.Message) MessageResponse {
	return MessageResponse{
		ID:              message.ExternalID,
		ThreadID:        message.ThreadExternalID,
		SenderID:        message.SenderExternalID,
		Seq:             message.Seq,
		ClientMessageID: message.ClientMessageID,
		Kind:            message.Kind,
		ContentFormat:   message.ContentFormat,
		Content:         message.Content,
		CreatedAt:       message.CreatedAt,
	}
}

func ToMessageResponses(messages []model.Message) []MessageResponse {
	responses := make([]MessageResponse, len(messages))
	for i, message := range messages {
		responses[i] = ToMessageResponse(message)
	}

	return responses
}
