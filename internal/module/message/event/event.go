package event

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/module/message/model"
)

const MessageCreatedType = "message.created"

type MessageCreated struct {
	MessageID         string
	ThreadID          string
	ThreadKind        string
	MembershipVersion int64 // Internal publishing metadata; not required by the stream consumer.
	SenderID          string
	RecipientIDs      []string
	Seq               int64
	Kind              string
	ContentFormat     string
	Content           string
	CreatedAt         time.Time
}

func FromMessage(message model.Message) MessageCreated {
	return MessageCreated{
		MessageID: message.ExternalID, ThreadID: message.ThreadExternalID, ThreadKind: message.ThreadKind,
		MembershipVersion: message.MembershipVersion,
		SenderID:          message.SenderExternalID, RecipientIDs: message.RecipientIDs,
		Seq: message.Seq, Kind: message.Kind, ContentFormat: message.ContentFormat,
		Content: message.Content, CreatedAt: message.CreatedAt,
	}
}
