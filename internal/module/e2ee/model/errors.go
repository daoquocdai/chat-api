package model

import "errors"

var (
	ErrInvalidPrekeys = errors.New("invalid public prekey bundle")
	ErrInvalidClaim   = errors.New("claim requires canonical non-zero user and thread UUIDs")
	ErrKeyConflict    = errors.New("registered identity or prekeys conflict")
	ErrThreadMode     = errors.New("claim requires a direct E2EE thread")
	ErrForbidden      = errors.New("claim requires two active peer participants")
	ErrBundleNotFound = errors.New("public prekey bundle not found")
)
