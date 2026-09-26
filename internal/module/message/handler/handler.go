package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	authmiddleware "github.com/daoquocdai/chat-api/internal/middleware"
	"github.com/daoquocdai/chat-api/internal/module/message/dto"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/gin-gonic/gin"
)

type MessageService interface {
	Send(
		ctx context.Context,
		actorExternalID, threadExternalID, clientMessageID, content string,
	) (model.Message, bool, error)
	List(
		ctx context.Context,
		actorExternalID, threadExternalID string,
		beforeSeq *int64,
		limit int,
	) (model.Page, error)
}

type Handler struct {
	service MessageService
}

func New(service MessageService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Send(c *gin.Context) {
	actorExternalID, ok := authenticatedUserID(c)
	if !ok || actorExternalID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var request dto.SendMessageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	message, created, err := h.service.Send(
		c.Request.Context(),
		actorExternalID,
		c.Param("id"),
		request.ClientMessageID,
		request.Content,
	)
	if err != nil {
		writeError(c, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.JSON(status, dto.ToMessageResponse(message))
}

func (h *Handler) List(c *gin.Context) {
	actorExternalID, ok := authenticatedUserID(c)
	if !ok || actorExternalID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	beforeSeq, limit, err := listQuery(c)
	if err != nil {
		writeError(c, err)
		return
	}

	page, err := h.service.List(
		c.Request.Context(),
		actorExternalID,
		c.Param("id"),
		beforeSeq,
		limit,
	)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.ToMessagePageResponse(page))
}

func listQuery(c *gin.Context) (*int64, int, error) {
	limit := model.DefaultPageLimit
	query := c.Request.URL.Query()

	if values, ok := query["limit"]; ok {
		if len(values) != 1 {
			return nil, 0, model.ErrInvalidLimit
		}
		parsed, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil || parsed < 1 || parsed > model.MaximumPageLimit {
			return nil, 0, model.ErrInvalidLimit
		}
		limit = int(parsed)
	}

	var beforeSeq *int64
	if values, ok := query["before_seq"]; ok {
		if len(values) != 1 {
			return nil, 0, model.ErrInvalidBeforeSeq
		}
		parsed, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || parsed <= 0 {
			return nil, 0, model.ErrInvalidBeforeSeq
		}
		beforeSeq = &parsed
	}

	return beforeSeq, limit, nil
}

func authenticatedUserID(c *gin.Context) (string, bool) {
	return authmiddleware.AuthenticatedUserID(c)
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrThreadIDRequired),
		errors.Is(err, model.ErrInvalidThreadID),
		errors.Is(err, model.ErrClientMessageIDRequired),
		errors.Is(err, model.ErrInvalidClientMessageID),
		errors.Is(err, model.ErrInvalidContent),
		errors.Is(err, model.ErrInvalidBeforeSeq),
		errors.Is(err, model.ErrInvalidLimit):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

	case errors.Is(err, threadmodel.ErrThreadNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": threadmodel.ErrThreadNotFound.Error()})

	case errors.Is(err, threadmodel.ErrNotParticipant):
		c.JSON(http.StatusForbidden, gin.H{"error": threadmodel.ErrNotParticipant.Error()})

	case errors.Is(err, usermodel.ErrUserNotFound), errors.Is(err, usermodel.ErrInvalidUserID):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})

	default:
		log.Printf("message handler: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
