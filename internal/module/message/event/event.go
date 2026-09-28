package event

import "time"

const MessageCreatedType = "message.created"

type MessageCreated struct {
	MessageID     string
	ThreadID      string
	SenderID      string
	RecipientID   string
	Seq           int64
	Kind          string
	ContentFormat string
	Content       string
	CreatedAt     time.Time
}
