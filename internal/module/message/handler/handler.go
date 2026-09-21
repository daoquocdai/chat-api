package handler

import (
	"encoding/json"
	"net/http"

	"github.com/daoquocdai/chat-api/internal/module/message"
	"github.com/daoquocdai/chat-api/internal/module/message/dto"
	"github.com/gin-gonic/gin"
)

type MessageService interface {
	Create(sender, receiver, content string) (message.Message, error)
	List() []message.Message
}

type Handler struct {
	service MessageService
}

func New(messageService MessageService) *Handler {
	return &Handler{service: messageService}
}

func (h *Handler) Create(c *gin.Context) {
	var request dto.CreateMessageRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	message, err := h.service.Create(
		request.Sender,
		request.Receiver,
		request.Content,
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, dto.ToMessageResponse(message))
}

func (h *Handler) List(c *gin.Context) {
	messages := h.service.List()
	c.JSON(http.StatusOK, dto.ToMessageResponses(messages))
}
