package handler

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/daoquocdai/chat-api/internal/module/message/dto"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/gin-gonic/gin"
)

type MessageService interface {
	Create(ctx context.Context, senderExternalID, receiverExternalID, content string) (model.Message, error)
	ListBetween(ctx context.Context, userExternalID, peerExternalID string) ([]model.Message, error)
}

type Handler struct {
	service MessageService
}

func New(service MessageService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Create(c *gin.Context) {
	var request dto.CreateMessageRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	message, err := h.service.Create(
		c.Request.Context(),
		request.SenderID,
		request.ReceiverID,
		request.Content,
	)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.ToMessageResponse(message))
}

func (h *Handler) ListBetween(c *gin.Context) {
	messages, err := h.service.ListBetween(
		c.Request.Context(),
		c.Query("user_id"),
		c.Query("peer_id"),
	)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.ToMessageResponses(messages))
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrUserIDsRequired),
		errors.Is(err, model.ErrSameUser),
		errors.Is(err, model.ErrInvalidContent),
		errors.Is(err, usermodel.ErrInvalidUserID):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

	case errors.Is(err, usermodel.ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": usermodel.ErrUserNotFound.Error()})

	default:
		log.Printf("message handler: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
