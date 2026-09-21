package model

import "time"

type User struct {
	ID         int64
	ExternalID string
	Username   string
	CreatedAt  time.Time
}
