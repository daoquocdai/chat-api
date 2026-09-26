package model

import "time"

type Peer struct {
	ExternalID string
	Username   string
}

type LastMessage struct {
	Seq              int64
	SenderExternalID string
	Content          string
	CreatedAt        time.Time
}

type Thread struct {
	ID              int64
	ExternalID      string
	Kind            string
	Peer            Peer
	LastSeq         int64
	LastReadSeq     int64
	PeerLastReadSeq int64
	UnreadCount     int64
	LastMessage     *LastMessage
	CreatedAt       time.Time
}
