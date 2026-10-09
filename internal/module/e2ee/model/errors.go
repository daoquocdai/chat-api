package model

import "errors"

var (
	ErrInvalidClaim    = errors.New("claim requires canonical non-zero user and thread UUIDs")
	ErrNotDirectThread = errors.New("this operation requires a direct thread")
	ErrForbidden       = errors.New("this operation requires active peer participation")
	ErrBundleNotFound  = errors.New("public prekey bundle not found")
	ErrInvalidEpoch    = errors.New("invalid key epoch or encrypted key backup")
	ErrEpochNotFound   = errors.New("key epoch not found")
	ErrEpochConflict   = errors.New("key epoch conflicts with the committed thread state")
)
