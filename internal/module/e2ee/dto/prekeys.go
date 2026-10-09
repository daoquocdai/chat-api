package dto

import (
	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
)

// Only public HTTP wire types belong here, never local private-key DTOs.
type ClaimRequest = e2ee.ClaimRequest
type Bundle = e2ee.Bundle

type AccountResponse struct {
	UserID       string               `json:"user_id"`
	Username     string               `json:"username"`
	KDF          e2ee.KDFProfile      `json:"kdf"`
	PublicBundle e2ee.UploadRequest   `json:"public_bundle"`
	AccountVault e2ee.EncryptedRecord `json:"account_vault"`
}

type EpochResponse struct {
	EpochID     string                `json:"epoch_id"`
	ThreadID    string                `json:"thread_id"`
	SenderID    string                `json:"sender_id"`
	RecipientID string                `json:"recipient_id"`
	Bootstrap   string                `json:"bootstrap"`
	KeyBackup   *e2ee.EncryptedRecord `json:"key_backup"`
}

type EpochPageResponse struct {
	Epochs         []EpochResponse `json:"epochs"`
	CurrentEpochID *string         `json:"current_epoch_id"`
}

type CreateEpochRequest struct {
	EpochID         string               `json:"epoch_id"`
	PreviousEpochID *string              `json:"previous_epoch_id"`
	Bootstrap       string               `json:"bootstrap"`
	KeyBackup       e2ee.EncryptedRecord `json:"key_backup"`
}

type BackupRequest struct {
	KeyBackup e2ee.EncryptedRecord `json:"key_backup"`
}

// A concurrent proposal returns the committed epoch the client must recover.
type EpochConflictError struct {
	Epoch EpochResponse
}

func (e *EpochConflictError) Error() string { return model.ErrEpochConflict.Error() }
func (e *EpochConflictError) Unwrap() error { return model.ErrEpochConflict }
