package model

import "time"

type Message struct {
	ID                 int64
	ExternalID         string
	SenderExternalID   string
	ReceiverExternalID string
	Content            string
	CreatedAt          time.Time
}
