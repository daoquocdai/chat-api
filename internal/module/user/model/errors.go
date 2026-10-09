package model

import "errors"

var (
	ErrUserNotFound          = errors.New("user not found")
	ErrUsernameTaken         = errors.New("username already exists")
	ErrInvalidUserID         = errors.New("invalid user ID")
	ErrInvalidUsername       = errors.New("username must contain between 1 and 50 Unicode characters and cannot contain NUL")
	ErrInvalidAuthCredential = errors.New("auth credential must be canonical Base64 encoding of 32 bytes")
	ErrInvalidAccountData    = errors.New("invalid account cryptographic data")
	ErrInvalidCredentials    = errors.New("invalid username or password")
)
