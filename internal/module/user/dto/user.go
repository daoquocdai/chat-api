package dto

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
)

type RegisterRequest struct {
	Username       string               `json:"username"`
	AuthCredential string               `json:"auth_credential"`
	KDF            e2ee.KDFProfile      `json:"kdf"`
	PublicBundle   e2ee.UploadRequest   `json:"public_bundle"`
	AccountVault   e2ee.EncryptedRecord `json:"account_vault"`
}

type LoginRequest struct {
	Username       string `json:"username"`
	AuthCredential string `json:"auth_credential"`
}

func (r *RegisterRequest) UnmarshalJSON(body []byte) error {
	type wire RegisterRequest
	var value wire
	if err := decodeRequest(body, &value, "username", "auth_credential", "kdf", "public_bundle", "account_vault"); err != nil {
		return err
	}
	*r = RegisterRequest(value)
	return nil
}

func (r *LoginRequest) UnmarshalJSON(body []byte) error {
	type wire LoginRequest
	var value wire
	if err := decodeRequest(body, &value, "username", "auth_credential"); err != nil {
		return err
	}
	*r = LoginRequest(value)
	return nil
}

// Auth requests reject ambiguous fields and the legacy raw-password contract.
func decodeRequest(body []byte, destination any, names ...string) error {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return errors.New("invalid JSON request")
	}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		seen, allowedName := allowed[name]
		if err != nil || !ok || !allowedName || seen {
			return errors.New("invalid JSON request field")
		}
		allowed[name] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return errors.New("invalid JSON request")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("invalid trailing JSON")
	}
	for _, present := range allowed {
		if !present {
			return errors.New("missing JSON request field")
		}
	}
	return json.Unmarshal(body, destination)
}

type UserResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type UserSummaryResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	UserID      string `json:"user_id"`
}

func ToUserResponse(user model.User) UserResponse {
	return UserResponse{
		ID:        user.ExternalID,
		Username:  user.Username,
		CreatedAt: user.CreatedAt,
	}
}

func ToUserSummaryResponses(users []model.User) []UserSummaryResponse {
	responses := make([]UserSummaryResponse, len(users))
	for i, user := range users {
		responses[i] = UserSummaryResponse{
			ID:       user.ExternalID,
			Username: user.Username,
		}
	}

	return responses
}
