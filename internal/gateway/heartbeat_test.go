package gateway

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/google/uuid"
)

func TestIdleConnectionReceivesHeartbeat(t *testing.T) {
	manager, err := token.NewJWT("gateway-test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub()
	hub.heartbeatInterval = 20 * time.Millisecond
	server := httptest.NewServer(NewHandler(hub, manager, nil, nil))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	userID := uuid.NewString()
	conn := dial(t, endpoint, "Bearer "+testJWT(t, "gateway-test-secret", time.Hour, userID))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	messageType, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.MessageText || string(data) != `{"type":"heartbeat"}` {
		t.Fatalf("idle frame = type %v data %q", messageType, data)
	}
}

