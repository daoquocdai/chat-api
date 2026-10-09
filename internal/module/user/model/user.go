package model

import (
	"time"

	"github.com/daoquocdai/chat-api/internal/e2ee"
)

type User struct {
	ID         int64
	ExternalID string
	Username   string
	CreatedAt  time.Time
}

type Credentials struct {
	User               User
	AuthCredentialHash string
}

type AccountRegistration struct {
	Username           string
	AuthCredentialHash string
	KDF                e2ee.KDFProfile
	PublicBundle       e2ee.UploadRequest
	AccountVault       e2ee.EncryptedRecord
}

type AuthParams struct {
	Username string          `json:"username"`
	KDF      e2ee.KDFProfile `json:"kdf"`
}

type LoginResult struct {
	AccessToken string
	UserID      string
}
