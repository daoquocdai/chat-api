package dto

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/module/thread/model"
)

type CreateDirectRequest struct {
	PeerID string `json:"peer_id"`
}

type PeerResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type LastMessageResponse struct {
	Seq       int64     `json:"seq"`
	SenderID  string    `json:"sender_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type ThreadResponse struct {
	ID          string               `json:"id"`
	Kind        string               `json:"kind"`
	Peer        PeerResponse         `json:"peer"`
	LastSeq     int64                `json:"last_seq"`
	LastMessage *LastMessageResponse `json:"last_message"`
	CreatedAt   time.Time            `json:"created_at"`
}

func ToThreadResponse(thread model.Thread) ThreadResponse {
	response := ThreadResponse{
		ID:        thread.ExternalID,
		Kind:      thread.Kind,
		Peer:      PeerResponse{ID: thread.Peer.ExternalID, Username: thread.Peer.Username},
		LastSeq:   thread.LastSeq,
		CreatedAt: thread.CreatedAt,
	}

	if thread.LastMessage != nil {
		response.LastMessage = &LastMessageResponse{
			Seq:       thread.LastMessage.Seq,
			SenderID:  thread.LastMessage.SenderExternalID,
			Content:   thread.LastMessage.Content,
			CreatedAt: thread.LastMessage.CreatedAt,
		}
	}

	return response
}

func ToThreadResponses(threads []model.Thread) []ThreadResponse {
	responses := make([]ThreadResponse, len(threads))
	for i, thread := range threads {
		responses[i] = ToThreadResponse(thread)
	}

	return responses
}
