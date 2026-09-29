package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
)

const queueSize = 64
const writeTimeout = 5 * time.Second
const pingInterval = 30 * time.Second
const defaultHeartbeatInterval = 5 * time.Second

var heartbeatFrame = []byte(`{"type":"heartbeat"}`)

type Hub struct {
	mu                sync.RWMutex
	clients           map[string]map[*client]struct{}
	heartbeatInterval time.Duration
}

type client struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte
	done   chan struct{}
	once   sync.Once
}

func NewHub() *Hub {
	return &Hub{
		clients:           make(map[string]map[*client]struct{}),
		heartbeatInterval: defaultHeartbeatInterval,
	}
}

func (h *Hub) add(userID string, conn *websocket.Conn) *client {
	c := &client{userID: userID, conn: conn, send: make(chan []byte, queueSize), done: make(chan struct{})}
	h.mu.Lock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*client]struct{})
	}
	h.clients[userID][c] = struct{}{}
	h.mu.Unlock()
	go h.writeLoop(c)
	return c
}

func (h *Hub) remove(c *client) {
	c.once.Do(func() {
		close(c.done)
		h.mu.Lock()
		delete(h.clients[c.userID], c)
		if len(h.clients[c.userID]) == 0 {
			delete(h.clients, c.userID)
		}
		h.mu.Unlock()
		_ = c.conn.CloseNow()
	})
}

func (h *Hub) writeLoop(c *client) {
	pingTicker := time.NewTicker(pingInterval)
	heartbeatTicker := time.NewTicker(h.heartbeatInterval)
	defer pingTicker.Stop()
	defer heartbeatTicker.Stop()
	defer h.remove(c)
	write := func(data []byte) error {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		return c.conn.Write(ctx, websocket.MessageText, data)
	}
	for {
		select {
		case <-c.done:
			return
		case data := <-c.send:
			if err := write(data); err != nil {
				return
			}
		case <-heartbeatTicker.C:
			if err := write(heartbeatFrame); err != nil {
				return
			}
		case <-pingTicker.C:
			ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

type wireMessage struct {
	Type          string    `json:"type"`
	MessageID     string    `json:"message_id"`
	ThreadID      string    `json:"thread_id"`
	SenderID      string    `json:"sender_id"`
	RecipientID   string    `json:"recipient_id"`
	Seq           int64     `json:"seq"`
	Kind          string    `json:"kind"`
	ContentFormat string    `json:"content_format"`
	Content       string    `json:"content"`
	CreatedAt     time.Time `json:"created_at"`
}

// Dispatch queues an event for every current recipient connection. Queueing is
// deliberately non-blocking; a slow connection is closed and must catch up via REST.
// This is not a delivery acknowledgement and does not deduplicate stream entries.
func (h *Hub) Dispatch(event messageevent.MessageCreated) error {
	data, err := json.Marshal(wireMessage{
		Type: messageevent.MessageCreatedType, MessageID: event.MessageID,
		ThreadID: event.ThreadID, SenderID: event.SenderID, RecipientID: event.RecipientID,
		Seq: event.Seq, Kind: event.Kind, ContentFormat: event.ContentFormat,
		Content: event.Content, CreatedAt: event.CreatedAt,
	})
	if err != nil {
		return err
	}
	h.mu.RLock()
	var slow []*client
	for c := range h.clients[event.RecipientID] {
		select {
		case <-c.done:
		case c.send <- data:
		default:
			slow = append(slow, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range slow {
		h.remove(c)
	}
	return nil
}
