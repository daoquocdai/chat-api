package dto

import "github.com/daoquocdai/chat-api/internal/module/message"

type CreateMessageRequest struct {
	Sender   string `json:"sender"`
	Receiver string `json:"receiver"`
	Content  string `json:"content"`
}

type MessageResponse struct {
	ID       int    `json:"id"`
	Sender   string `json:"sender"`
	Receiver string `json:"receiver"`
	Content  string `json:"content"`
}

func ToMessageResponse(item message.Message) MessageResponse {
	return MessageResponse{
		ID:       item.ID,
		Sender:   item.Sender,
		Receiver: item.Receiver,
		Content:  item.Content,
	}
}

func ToMessageResponses(messages []message.Message) []MessageResponse {
	responses := make([]MessageResponse, len(messages))
	for i, item := range messages {
		responses[i] = ToMessageResponse(item)
	}
	return responses
}
