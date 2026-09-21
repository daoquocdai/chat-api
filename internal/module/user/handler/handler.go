package handler

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/daoquocdai/chat-api/internal/module/user/dto"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/gin-gonic/gin"
)

type UserService interface {
	Create(
		ctx context.Context,
		username string,
	) (model.User, error)

	GetByExternalID(
		ctx context.Context,
		externalID string,
	) (model.User, error)
}

type Handler struct {
	service UserService
}

func New(service UserService) *Handler {
	return &Handler{
		service: service,
	}
}

func (h *Handler) Create(c *gin.Context) {
	var request dto.CreateUserRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid JSON body",
		})
		return
	}

	user, err := h.service.Create(
		c.Request.Context(),
		request.Username,
	)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.ToUserResponse(user))
}

func (h *Handler) GetByExternalID(c *gin.Context) {
	externalID := c.Param("id")

	user, err := h.service.GetByExternalID(
		c.Request.Context(),
		externalID,
	)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.ToUserResponse(user))
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidUsername),
		errors.Is(err, model.ErrInvalidUserID):
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})

	case errors.Is(err, model.ErrUsernameTaken):
		c.JSON(http.StatusConflict, gin.H{
			"error": model.ErrUsernameTaken.Error(),
		})

	case errors.Is(err, model.ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{
			"error": model.ErrUserNotFound.Error(),
		})

	default:
		log.Printf("user handler: %v", err)

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "internal server error",
		})
	}
}
