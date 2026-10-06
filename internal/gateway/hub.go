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
	stopping          bool
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup // Handlers (including upgrades), writers and connection cleanup.
	done              chan struct{}
}

type client struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte
	done   chan struct{}
	once   sync.Once
}

func NewHub() *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		clients:           make(map[string]map[*client]struct{}),
		heartbeatInterval: defaultHeartbeatInterval,
		ctx:               ctx, cancel: cancel, done: make(chan struct{}),
	}
}

// begin tracks even an upgrade that is still authenticating when shutdown starts.
func (h *Hub) begin() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopping {
		return false
	}
	h.wg.Add(1)
	return true
}

func (h *Hub) add(userID string, conn *websocket.Conn) *client {
	c := &client{userID: userID, conn: conn, send: make(chan []byte, queueSize), done: make(chan struct{})}
	h.mu.Lock()
	if h.stopping {
		h.mu.Unlock()
		return nil
	}
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*client]struct{})
	}
	h.clients[userID][c] = struct{}{}
	h.wg.Add(1)
	h.mu.Unlock()
	go h.writeLoop(c)
	return c
}

// Stop prevents registrations before waiting, cancels network operations and closes
// sockets in parallel. No close handshake is needed: clients recover through REST.
// The returned channel includes the HTTP handlers and WebSocket reader cleanup;
// http.Server.Shutdown alone does not wait for hijacked connections.
func (h *Hub) Stop() <-chan struct{} {
	h.mu.Lock()
	if h.stopping {
		h.mu.Unlock()
		return h.done
	}
	h.stopping = true
	var clients []*client
	for _, connections := range h.clients {
		for c := range connections {
			clients = append(clients, c)
		}
	}
	h.wg.Add(len(clients))
	h.mu.Unlock()
	h.cancel()
	for _, c := range clients {
		go func() {
			defer h.wg.Done()
			h.remove(c)
		}()
	}
	go func() {
		h.wg.Wait()
		close(h.done)
	}()
	return h.done
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
	defer h.wg.Done()
	pingTicker := time.NewTicker(pingInterval)
	heartbeatTicker := time.NewTicker(h.heartbeatInterval)
	defer pingTicker.Stop()
	defer heartbeatTicker.Stop()
	defer h.remove(c)
	write := func(data []byte) error {
		ctx, cancel := context.WithTimeout(h.ctx, writeTimeout)
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
			ctx, cancel := context.WithTimeout(h.ctx, writeTimeout)
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
	ThreadKind    string    `json:"thread_kind"`
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
	for _, recipientID := range event.RecipientIDs {
		if err := h.dispatchTo(event, recipientID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) dispatchTo(event messageevent.MessageCreated, recipientID string) error {
	data, err := json.Marshal(wireMessage{
		Type: messageevent.MessageCreatedType, MessageID: event.MessageID,
		ThreadID: event.ThreadID, ThreadKind: event.ThreadKind, SenderID: event.SenderID, RecipientID: recipientID,
		Seq: event.Seq, Kind: event.Kind, ContentFormat: event.ContentFormat,
		Content: event.Content, CreatedAt: event.CreatedAt,
	})
	if err != nil {
		return err
	}
	h.mu.RLock()
	if h.stopping {
		h.mu.RUnlock()
		return nil
	}
	var slow []*client
	for c := range h.clients[recipientID] {
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
