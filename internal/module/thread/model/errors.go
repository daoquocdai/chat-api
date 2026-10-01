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
	ErrInvalidGroupName     = errors.New("group name must contain between 1 and 100 Unicode characters and cannot contain NUL")
	ErrInvalidMemberIDs     = errors.New("member IDs must be nonempty and unique")
	ErrGroupTooLarge        = errors.New("a group supports at most 100 active members")
	ErrNotGroup             = errors.New("this operation requires a group thread")
	ErrAdminRequired        = errors.New("only a group admin can change other members")
	ErrMemberNotFound       = errors.New("user has never been a member of this group")
	ErrUseLeave             = errors.New("use the leave endpoint to leave a group")
)
