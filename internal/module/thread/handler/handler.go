package handler

import (
	"context"
	"errors"
	"log"
	"net/http"

	authmiddleware "github.com/daoquocdai/chat-api/internal/middleware"
	"github.com/daoquocdai/chat-api/internal/module/thread/dto"
	"github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/gin-gonic/gin"
)

type ThreadService interface {
	CreateOrGetDirect(ctx context.Context, actorExternalID, peerExternalID string) (model.Thread, bool, error)
	List(ctx context.Context, actorExternalID string) ([]model.Thread, error)
	MarkRead(ctx context.Context, actorExternalID, threadExternalID string, lastReadSeq int64) (int64, error)
}

type Handler struct {
	service ThreadService
}

func New(service ThreadService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) CreateOrGetDirect(c *gin.Context) {
	actorExternalID, ok := authenticatedUserID(c)
	if !ok || actorExternalID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var request dto.CreateDirectRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	thread, created, err := h.service.CreateOrGetDirect(
		c.Request.Context(),
		actorExternalID,
		request.PeerID,
	)
	if err != nil {
		writeError(c, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, dto.ToThreadResponse(thread))
}

func (h *Handler) List(c *gin.Context) {
	actorExternalID, ok := authenticatedUserID(c)
	if !ok || actorExternalID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	threads, err := h.service.List(c.Request.Context(), actorExternalID)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.ToThreadResponses(threads))
}

func (h *Handler) MarkRead(c *gin.Context) {
	actorExternalID, ok := authenticatedUserID(c)
	if !ok || actorExternalID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var request dto.MarkReadRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if request.LastReadSeq == nil {
		writeError(c, model.ErrReadSequenceRequired)
		return
	}

	lastReadSeq, err := h.service.MarkRead(
		c.Request.Context(),
		actorExternalID,
		c.Param("id"),
		*request.LastReadSeq,
	)
	if err != nil {
		if errors.Is(err, usermodel.ErrUserNotFound) || errors.Is(err, usermodel.ErrInvalidUserID) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.MarkReadResponse{LastReadSeq: lastReadSeq})
}

func authenticatedUserID(c *gin.Context) (string, bool) {
	return authmiddleware.AuthenticatedUserID(c)
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrPeerIDRequired), errors.Is(err, model.ErrSameUser),
		errors.Is(err, model.ErrThreadIDRequired), errors.Is(err, model.ErrInvalidThreadID),
		errors.Is(err, model.ErrReadSequenceRequired), errors.Is(err, model.ErrInvalidReadSequence),
		errors.Is(err, usermodel.ErrInvalidUserID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

	case errors.Is(err, usermodel.ErrUserNotFound), errors.Is(err, model.ErrThreadNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})

	case errors.Is(err, model.ErrNotParticipant):
		c.JSON(http.StatusForbidden, gin.H{"error": model.ErrNotParticipant.Error()})

	default:
		log.Printf("thread handler: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
