package wsticket

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/daoquocdai/chat-api/internal/middleware"
	"github.com/gin-gonic/gin"
)

type Issuer interface {
	Issue(ctx context.Context, userID string) (string, error)
}

type Handler struct {
	issuer Issuer
	wsURL  string
	limit  time.Duration
}

func NewHandler(issuer Issuer, wsURL string, limit time.Duration) *Handler {
	return &Handler{issuer: issuer, wsURL: wsURL, limit: limit}
}

func (h *Handler) Issue(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	userID, ok := middleware.AuthenticatedUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.limit)
	defer cancel()
	ticket, err := h.issuer.Issue(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrInvalidUser) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		log.Printf("issue WebSocket ticket: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "WebSocket ticket is temporarily unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ticket": ticket, "ws_url": h.wsURL})
}
