package model

import "errors"

var (
	ErrPeerIDRequired = errors.New("peer ID is required")
	ErrSameUser       = errors.New("cannot create a direct thread with yourself")
	ErrThreadNotFound = errors.New("thread not found")
	ErrNotParticipant = errors.New("user is not an active participant in this thread")
)
