package model

import "errors"

var (
	ErrUserIDsRequired = errors.New("two user IDs are required")
	ErrSameUser        = errors.New("sender and receiver must be different users")
	ErrInvalidContent  = errors.New("content must contain between 1 and 1000 Unicode characters")
)
