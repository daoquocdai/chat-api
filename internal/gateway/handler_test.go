package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/daoquocdai/chat-api/internal/wsticket"
	"github.com/google/uuid"
)

func testJWT(t *testing.T, secret string, ttl time.Duration, subject string) string {
	t.Helper()
	manager, err := token.NewJWT(secret, ttl)
	if err != nil {
		t.Fatal(err)
	}
	value, err := manager.Create(subject)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type oneTimeTicket struct {
	mu     sync.Mutex
	ticket string
	userID string
}

func (s *oneTimeTicket) Consume(_ context.Context, ticket string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ticket != s.ticket || s.ticket == "" {
		return "", wsticket.ErrInvalidTicket
	}
	s.ticket = ""
	return s.userID, nil
}

func TestTicketHandshakeBeforeUpgradeAndReplayRejected(t *testing.T) {
	manager, err := token.NewJWT("gateway-test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	bobID := uuid.NewString()
	tickets := &oneTimeTicket{ticket: "one-time-ticket", userID: bobID}
	hub := NewHub()
	server := httptest.NewServer(NewHandler(hub, manager, tickets, []string{"localhost:8080"}))
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "?ticket=one-time-ticket"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	headers := http.Header{"Origin": []string{"http://localhost:8080"}}
	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("ticket handshake: %v response=%v", err, response)
	}
	awaitClients(t, hub, bobID, 1)
	_ = conn.CloseNow()
	conn, response, err = websocket.Dial(ctx, endpoint, &websocket.DialOptions{HTTPHeader: headers})
	if conn != nil {
		_ = conn.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ticket replay was not rejected before upgrade: response=%v err=%v", response, err)
	}
}

func testServer(t *testing.T) (*Hub, string) {
	t.Helper()
	manager, err := token.NewJWT("gateway-test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub()
	server := httptest.NewServer(NewHandler(hub, manager, nil, nil))
	t.Cleanup(server.Close)
	return hub, "ws" + strings.TrimPrefix(server.URL, "http")
}

func dial(t *testing.T, endpoint, auth string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{auth}},
	})
	if err != nil {
		t.Fatalf("dial: %v (response: %v)", err, response)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func awaitClients(t *testing.T, hub *Hub, userID string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		count := len(hub.clients[userID])
		hub.mu.RUnlock()
		if count == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("user %s did not reach %d connections", userID, want)
}

func readMessage(t *testing.T, conn *websocket.Conn) wireMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var message wireMessage
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func TestHandshakeJWTBeforeUpgrade(t *testing.T) {
	_, endpoint := testServer(t)
	subject := uuid.NewString()
	expired := testJWT(t, "gateway-test-secret", time.Second, subject)
	time.Sleep(1100 * time.Millisecond)
	tests := []struct{ name, auth string }{
		{"missing", ""},
		{"malformed bearer", "Basic abc"},
		{"bad signature", "Bearer " + testJWT(t, "other-secret", time.Hour, subject)},
		{"expired", "Bearer " + expired},
		{"non-UUID sub", "Bearer " + testJWT(t, "gateway-test-secret", time.Hour, "not-a-uuid")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
				HTTPHeader: http.Header{"Authorization": []string{tt.auth}},
			})
			if conn != nil {
				_ = conn.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("got conn=%v response=%v err=%v, want 401 before upgrade", conn, response, err)
			}
		})
	}
	conn := dial(t, endpoint, "Bearer "+testJWT(t, "gateway-test-secret", time.Hour, subject))
	if conn == nil {
		t.Fatal("valid JWT was not accepted")
	}
}

func TestFanoutRecipientOnlyAndDuplicateEvents(t *testing.T) {
	hub, endpoint := testServer(t)
	bobID, aliceID := uuid.NewString(), uuid.NewString()
	bobToken := testJWT(t, "gateway-test-secret", time.Hour, bobID)
	bob1 := dial(t, endpoint, "Bearer "+bobToken)
	bob2 := dial(t, endpoint, "Bearer "+bobToken)
	alice := dial(t, endpoint, "Bearer "+testJWT(t, "gateway-test-secret", time.Hour, aliceID))
	awaitClients(t, hub, bobID, 2)
	awaitClients(t, hub, aliceID, 1)
	event := messageevent.MessageCreated{
		MessageID: uuid.NewString(), ThreadID: uuid.NewString(), SenderID: aliceID,
		RecipientID: bobID, Seq: 9, Kind: "text", ContentFormat: "plaintext",
		Content: "hello", CreatedAt: time.Now().UTC(),
	}
	for range 2 {
		if err := hub.Dispatch(event); err != nil {
			t.Fatal(err)
		}
	}
	for _, conn := range []*websocket.Conn{bob1, bob2} {
		for range 2 {
			got := readMessage(t, conn)
			if got.MessageID != event.MessageID || got.ThreadID != event.ThreadID ||
				got.Seq != event.Seq || got.Content != event.Content || got.Type != messageevent.MessageCreatedType {
				t.Fatalf("unexpected WS event: %+v", got)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, _, err := alice.Read(ctx); err == nil {
		t.Fatal("event leaked to Alice")
	}
	if err := hub.Dispatch(messageevent.MessageCreated{RecipientID: uuid.NewString()}); err != nil {
		t.Fatalf("offline recipient must not block: %v", err)
	}
	_ = bob1.CloseNow()
	awaitClients(t, hub, bobID, 1)
}
