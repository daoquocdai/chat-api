package event

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/module/message/model"
)

const MessageCreatedType = "message.created"

type MessageCreated struct {
	MessageID     string
	ThreadID      string
	ThreadKind    string
	SenderID      string
	RecipientIDs  []string
	Seq           int64
	Kind          string
	ContentFormat string
	Content       string
	CreatedAt     time.Time
}

func FromMessage(message model.Message, recipients []string) MessageCreated {
	return MessageCreated{
		MessageID: message.ExternalID, ThreadID: message.ThreadExternalID, ThreadKind: message.ThreadKind,
		SenderID: message.SenderExternalID, RecipientIDs: recipients,
		Seq: message.Seq, Kind: message.Kind, ContentFormat: message.ContentFormat,
		Content: message.Content, CreatedAt: message.CreatedAt,
	}
}
