package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/daoquocdai/chat-api/internal/e2ee"
	authmiddleware "github.com/daoquocdai/chat-api/internal/middleware"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const maximumBodyBytes = 128 * 1024

type E2EEService interface {
	Account(ctx context.Context, actorExternalID string) (dto.AccountResponse, error)
	Claim(ctx context.Context, actorExternalID, recipientExternalID string, request dto.ClaimRequest) (dto.Bundle, error)
	Epochs(ctx context.Context, actorExternalID, threadExternalID string) (dto.EpochPageResponse, error)
	CreateEpoch(ctx context.Context, actorExternalID, threadExternalID string, request dto.CreateEpochRequest) (dto.EpochResponse, bool, error)
	Backup(ctx context.Context, actorExternalID, threadExternalID, epochExternalID string, backup e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error)
}

type Handler struct{ service E2EEService }

func New(service E2EEService) *Handler { return &Handler{service: service} }

func (h *Handler) Account(c *gin.Context) {
	actor, ok := authenticatedActor(c)
	if !ok {
		return
	}
	account, err := h.service.Account(c.Request.Context(), actor)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, account)
}

func (h *Handler) Claim(c *gin.Context) {
	actor, ok := authenticatedActor(c)
	if !ok {
		return
	}
	var request dto.ClaimRequest
	if !readJSON(c, &request, []string{"thread_id"}, nil) {
		return
	}
	bundle, err := h.service.Claim(c.Request.Context(), actor, c.Param("user_id"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, bundle)
}

func (h *Handler) Epochs(c *gin.Context) {
	actor, ok := authenticatedActor(c)
	if !ok {
		return
	}
	page, err := h.service.Epochs(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, page)
}

func (h *Handler) CreateEpoch(c *gin.Context) {
	actor, ok := authenticatedActor(c)
	if !ok {
		return
	}
	var request dto.CreateEpochRequest
	if !readJSON(c, &request, []string{"epoch_id", "bootstrap", "key_backup"}, []string{"previous_epoch_id"}) {
		return
	}
	epoch, created, err := h.service.CreateEpoch(c.Request.Context(), actor, c.Param("id"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, epoch)
}

func (h *Handler) Backup(c *gin.Context) {
	actor, ok := authenticatedActor(c)
	if !ok {
		return
	}
	var request dto.BackupRequest
	if !readJSON(c, &request, []string{"key_backup"}, nil) {
		return
	}
	backup, err := h.service.Backup(c.Request.Context(), actor, c.Param("id"), c.Param("epoch_id"), request.KeyBackup)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.BackupRequest{KeyBackup: backup})
}

func authenticatedActor(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "no-store")
	actor, ok := authmiddleware.AuthenticatedUserID(c)
	parsed, err := uuid.Parse(actor)
	if !ok || err != nil || parsed == uuid.Nil || parsed.String() != actor {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return "", false
	}
	return actor, true
}

func readJSON(c *gin.Context, destination any, required, nullableOptional []string) bool {
	reader := http.MaxBytesReader(c.Writer, c.Request.Body, maximumBodyBytes)
	defer reader.Close()
	body, err := io.ReadAll(reader)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "body exceeds 128 KiB"})
		return false
	}
	if err != nil || !utf8.Valid(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}
	fields, err := object(body, required, nullableOptional)
	if err == nil && fields["key_backup"] != nil {
		_, err = object(fields["key_backup"], []string{"version", "nonce", "ciphertext"}, nil)
	}
	if err != nil || json.Unmarshal(body, destination) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return false
	}
	return true
}

// These endpoints use exact field names, reject duplicates and unknown fields,
// and allow null only for the optional previous epoch.
func object(raw []byte, required, nullableOptional []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, model.ErrInvalidEpoch
	}
	allowed := make(map[string]bool, len(required)+len(nullableOptional))
	for _, name := range required {
		allowed[name] = false
	}
	for _, name := range nullableOptional {
		allowed[name] = true
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		nullable, allowedName := allowed[name]
		if err != nil || !ok || !allowedName || fields[name] != nil {
			return nil, model.ErrInvalidEpoch
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || (!nullable && bytes.Equal(bytes.TrimSpace(value), []byte("null"))) {
			return nil, model.ErrInvalidEpoch
		}
		fields[name] = value
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, model.ErrInvalidEpoch
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, model.ErrInvalidEpoch
	}
	for _, name := range required {
		if fields[name] == nil {
			return nil, model.ErrInvalidEpoch
		}
	}
	return fields, nil
}

func writeError(c *gin.Context, err error) {
	var conflict *dto.EpochConflictError
	if errors.As(err, &conflict) {
		c.JSON(http.StatusConflict, gin.H{"error": model.ErrEpochConflict.Error(), "epoch": conflict.Epoch})
		return
	}
	switch {
	case errors.Is(err, usermodel.ErrInvalidUserID):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	case errors.Is(err, model.ErrInvalidClaim), errors.Is(err, model.ErrInvalidEpoch):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrEpochConflict), errors.Is(err, model.ErrNotDirectThread):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrForbidden), errors.Is(err, threadmodel.ErrNotParticipant):
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
	case errors.Is(err, model.ErrBundleNotFound), errors.Is(err, model.ErrEpochNotFound),
		errors.Is(err, usermodel.ErrUserNotFound), errors.Is(err, threadmodel.ErrThreadNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		// Database errors can contain encrypted key material. Never echo or log them.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
