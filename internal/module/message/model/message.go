package model

import "time"

type Message struct {
	ID               int64
	ExternalID       string
	ThreadExternalID string
	SenderExternalID string
	Seq              int64
	ClientMessageID  string
	Kind             string
	ContentFormat    string
	Content          string
	CreatedAt        time.Time
}
