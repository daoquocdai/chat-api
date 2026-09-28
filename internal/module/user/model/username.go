package model

import (
	"strings"
	"unicode/utf8"
)

func NormalizeUsername(username string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))

	if length := utf8.RuneCountInString(username); length == 0 || length > 50 || strings.ContainsRune(username, '\x00') {
		return "", ErrInvalidUsername
	}

	return username, nil
}
