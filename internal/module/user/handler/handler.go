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
	Register(ctx context.Context, username, password string) (model.User, error)
	Login(ctx context.Context, username, password string) (string, error)
	List(ctx context.Context) ([]model.User, error)
}

type Handler struct {
	service UserService
}

func New(service UserService) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(c *gin.Context) {
	var request dto.RegisterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	user, err := h.service.Register(c.Request.Context(), request.Username, request.Password)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.ToUserResponse(user))
}

func (h *Handler) Login(c *gin.Context) {
	var request dto.LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	accessToken, err := h.service.Login(c.Request.Context(), request.Username, request.Password)
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.TokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
	})
}

func (h *Handler) List(c *gin.Context) {
	users, err := h.service.List(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusOK, dto.ToUserSummaryResponses(users))
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidUsername),
		errors.Is(err, model.ErrInvalidUserID),
		errors.Is(err, model.ErrInvalidPassword):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})

	case errors.Is(err, model.ErrUsernameTaken):
		c.JSON(http.StatusConflict, gin.H{"error": model.ErrUsernameTaken.Error()})

	case errors.Is(err, model.ErrUserNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": model.ErrUserNotFound.Error()})

	case errors.Is(err, model.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": model.ErrInvalidCredentials.Error()})

	default:
		log.Printf("user handler: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
