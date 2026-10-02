package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/daoquocdai/chat-api/internal/wsticket"
	"github.com/google/uuid"
)

type TicketConsumer interface {
	Consume(ctx context.Context, ticket string) (string, error)
}

type Handler struct {
	hub     *Hub
	jwt     *token.JWT
	tickets TicketConsumer
	origins []string
}

func NewHandler(hub *Hub, jwt *token.JWT, tickets TicketConsumer, origins []string) *Handler {
	return &Handler{hub: hub, jwt: jwt, tickets: tickets, origins: origins}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.hub.begin() {
		http.Error(w, "gateway shutting down", http.StatusServiceUnavailable)
		return
	}
	defer h.hub.wg.Done()
	ctx, cancel := context.WithCancel(r.Context())
	stopCancel := context.AfterFunc(h.hub.ctx, cancel)
	defer stopCancel()
	defer cancel()
	r = r.WithContext(ctx)
	userID, err := h.authenticate(r)
	if err != nil {
		if errors.Is(err, wsticket.ErrInvalidTicket) || errors.Is(err, errUnauthorized) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		} else {
			http.Error(w, "WebSocket authentication temporarily unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	parsedID, err := uuid.Parse(userID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.origins})
	if err != nil {
		return
	}
	client := h.hub.add(parsedID.String(), conn)
	if client == nil {
		_ = conn.CloseNow()
		return
	}
	<-conn.CloseRead(r.Context()).Done()
	h.hub.remove(client)
}

var errUnauthorized = errors.New("unauthorized")

func (h *Handler) authenticate(r *http.Request) (string, error) {
	if header := r.Header.Get("Authorization"); header != "" {
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return "", errUnauthorized
		}
		userID, err := h.jwt.Verify(parts[1])
		if err != nil {
			return "", errUnauthorized
		}
		return userID, nil
	}
	if h.tickets == nil {
		return "", errUnauthorized
	}
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" {
		return "", errUnauthorized
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	return h.tickets.Consume(ctx, ticket)
}
