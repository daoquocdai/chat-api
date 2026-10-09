package model

import "github.com/daoquocdai/chat-api/internal/e2ee"

func NormalizeUsername(username string) (string, error) {
	username, err := e2ee.NormalizeAccountUsername(username)
	if err != nil {
		return "", ErrInvalidUsername
	}

	return username, nil
}
