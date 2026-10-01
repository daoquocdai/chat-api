package model

import "time"

type Message struct {
	ID                int64
	ExternalID        string
	ThreadExternalID  string
	ThreadKind        string
	MembershipVersion int64 // PostgreSQL boundary identifying the recipient snapshot at Seq.
	SenderExternalID  string
	RecipientIDs      []string
	Seq               int64
	Kind              string
	ContentFormat     string
	Content           string
	CreatedAt         time.Time
}

type Page struct {
	Messages   []Message
	NextCursor *int64
}
