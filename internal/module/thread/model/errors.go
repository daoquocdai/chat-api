package model

import "errors"

var (
	ErrPeerIDRequired       = errors.New("peer ID is required")
	ErrSameUser             = errors.New("cannot create a direct thread with yourself")
	ErrThreadIDRequired     = errors.New("thread ID is required")
	ErrInvalidThreadID      = errors.New("thread ID must be a UUID")
	ErrReadSequenceRequired = errors.New("last_read_seq is required")
	ErrInvalidReadSequence  = errors.New("last_read_seq is outside the visible thread range")
	ErrThreadNotFound       = errors.New("thread not found")
	ErrNotParticipant       = errors.New("user is not an active participant in this thread")
)
