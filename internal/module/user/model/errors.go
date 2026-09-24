package model

import "errors"

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrInvalidUserID      = errors.New("invalid user ID")
	ErrInvalidUsername    = errors.New("username must contain between 1 and 50 Unicode characters and cannot contain NUL")
	ErrInvalidPassword    = errors.New("password must contain at least 8 Unicode characters, must not exceed 72 bytes, and cannot contain NUL")
	ErrInvalidCredentials = errors.New("invalid username or password")
)
