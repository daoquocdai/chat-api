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

// MaxGroupMembers includes the creator.
const MaxGroupMembers = 100

type Thread struct {
	ID              int64
	ExternalID      string
	Kind            string
	Name            string
	Role            string
	MemberCount     int64
	Peer            Peer
	LastSeq         int64
	JoinedSeq       int64
	LastReadSeq     int64
	PeerLastReadSeq int64
	UnreadCount     int64
	LastMessage     *LastMessage
	CreatedAt       time.Time
}

type Member struct {
	ExternalID  string
	Username    string
	Role        string
	JoinedSeq   int64
	LastReadSeq int64
}

type MembershipAction string

const (
	AddMember    MembershipAction = "add"
	RemoveMember MembershipAction = "remove"
	LeaveGroup   MembershipAction = "leave"
)
