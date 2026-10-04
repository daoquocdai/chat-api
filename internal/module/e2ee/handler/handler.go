package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	authmiddleware "github.com/daoquocdai/chat-api/internal/middleware"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/gin-gonic/gin"
)

const maximumBodyBytes = 16 * 1024

type E2EEService interface {
	Upload(ctx context.Context, actorExternalID string, request dto.UploadRequest) (dto.UploadResult, error)
	Claim(ctx context.Context, actorExternalID, recipientExternalID string, request dto.ClaimRequest) (dto.Bundle, error)
}

type Handler struct{ service E2EEService }

func New(service E2EEService) *Handler { return &Handler{service: service} }

func (h *Handler) Upload(c *gin.Context) {
	actor, ok := authmiddleware.AuthenticatedUserID(c)
	if !ok || actor == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	body, ok := readBody(c)
	if !ok {
		return
	}
	request, err := decodeUpload(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	result, err := h.service.Upload(c.Request.Context(), actor, request)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Claim(c *gin.Context) {
	actor, ok := authmiddleware.AuthenticatedUserID(c)
	if !ok || actor == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	body, ok := readBody(c)
	if !ok {
		return
	}
	if _, err := object(body, []string{"thread_id"}); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	var request dto.ClaimRequest
	if json.Unmarshal(body, &request) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	bundle, err := h.service.Claim(c.Request.Context(), actor, c.Param("user_id"), request)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, bundle)
}

func readBody(c *gin.Context) ([]byte, bool) {
	reader := http.MaxBytesReader(c.Writer, c.Request.Body, maximumBodyBytes)
	defer reader.Close()
	body, err := io.ReadAll(reader)
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "body exceeds 16 KiB"})
		return nil, false
	}
	if err != nil || !utf8.Valid(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return nil, false
	}
	return body, true
}

// This decoder belongs only to the new endpoints. No global Gin setting.
// Exact names/presence plus duplicate and null rejection prevent ambiguous DTOs.
func object(raw []byte, required []string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return nil, model.ErrInvalidPrekeys
	}
	allowed := make(map[string]bool, len(required))
	for _, name := range required {
		allowed[name] = true
	}
	fields := make(map[string]json.RawMessage, len(required))
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] || fields[name] != nil {
			return nil, model.ErrInvalidPrekeys
		}
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, model.ErrInvalidPrekeys
		}
		fields[name] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return nil, model.ErrInvalidPrekeys
	}
	var extra any
	if d.Decode(&extra) != io.EOF || len(fields) != len(required) {
		return nil, model.ErrInvalidPrekeys
	}
	return fields, nil
}

func decodeUpload(body []byte) (dto.UploadRequest, error) {
	fields, err := object(body, []string{"identity_public_key", "signed_prekey", "one_time_prekeys"})
	if err != nil {
		return dto.UploadRequest{}, err
	}
	if _, err := object(fields["signed_prekey"], []string{"key_id", "public_key", "signature"}); err != nil {
		return dto.UploadRequest{}, err
	}
	var opks []json.RawMessage
	if json.Unmarshal(fields["one_time_prekeys"], &opks) != nil || len(opks) > 100 {
		return dto.UploadRequest{}, model.ErrInvalidPrekeys
	}
	for _, opk := range opks {
		if _, err := object(opk, []string{"key_id", "public_key"}); err != nil {
			return dto.UploadRequest{}, err
		}
	}
	var request dto.UploadRequest
	if json.Unmarshal(body, &request) != nil {
		return dto.UploadRequest{}, model.ErrInvalidPrekeys
	}
	return request, nil
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usermodel.ErrInvalidUserID):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	case errors.Is(err, model.ErrInvalidPrekeys), errors.Is(err, model.ErrInvalidClaim):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrKeyConflict), errors.Is(err, model.ErrThreadMode):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, model.ErrForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": model.ErrForbidden.Error()})
	case errors.Is(err, model.ErrBundleNotFound), errors.Is(err, usermodel.ErrUserNotFound), errors.Is(err, threadmodel.ErrThreadNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		// Raw DB errors may contain bytea values. Never log or echo them.
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
