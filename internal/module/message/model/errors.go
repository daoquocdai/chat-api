package model

import "errors"

var (
	ErrThreadIDRequired        = errors.New("thread ID is required")
	ErrInvalidThreadID         = errors.New("thread ID must be a UUID")
	ErrClientMessageIDRequired = errors.New("client_msg_id is required")
	ErrInvalidClientMessageID  = errors.New("client_msg_id must be a UUID")
	ErrInvalidContent          = errors.New("content must be valid UTF-8, contain between 1 and 1000 Unicode characters, and cannot contain NUL")
)
