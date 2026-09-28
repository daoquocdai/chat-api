package gateway

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/daoquocdai/chat-api/internal/module/message/publisher"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/daoquocdai/chat-api/internal/wsticket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"net/http/httptest"
	"strings"
)

func TestDecodeRejectsIncompleteEntry(t *testing.T) {
	for _, values := range []map[string]interface{}{
		{"event": "other"},
		{"event": "message.created", "recipient_id": "bob"},
	} {
		if _, err := decode(values); err == nil {
			t.Fatalf("accepted invalid entry: %v", values)
		}
	}
}

type flakyStream struct {
	redis.Cmdable
	reads  atomic.Int32
	acked  chan struct{}
	cancel context.CancelFunc
}

func (f *flakyStream) XGroupCreateMkStream(context.Context, string, string, string) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(context.Background())
	cmd.SetVal("OK")
	return cmd
}

func (f *flakyStream) XReadGroup(_ context.Context, args *redis.XReadGroupArgs) *redis.XStreamSliceCmd {
	if args.Streams[1] == "0" {
		return redis.NewXStreamSliceCmdResult(nil, redis.Nil)
	}
	if f.reads.Add(1) == 1 {
		return redis.NewXStreamSliceCmdResult(nil, errors.New("temporary Redis failure"))
	}
	return redis.NewXStreamSliceCmdResult([]redis.XStream{{Messages: []redis.XMessage{{ID: "1-0", Values: testValues("bob")}}}}, nil)
}

func (f *flakyStream) XAck(context.Context, string, string, ...string) *redis.IntCmd {
	cmd := redis.NewIntCmd(context.Background())
	cmd.SetVal(1)
	close(f.acked)
	f.cancel()
	return cmd
}

func TestConsumerRetriesTransientRedisError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	fake := &flakyStream{acked: make(chan struct{}), cancel: cancel}
	consumer := NewConsumer(fake, NewHub(), "test-stream", "test-group", "gateway-1", log.New(io.Discard, "", 0))
	done := make(chan struct{})
	go func() { consumer.Run(ctx); close(done) }()
	select {
	case <-fake.acked:
		if fake.reads.Load() < 2 {
			t.Fatal("consumer did not retry")
		}
	case <-ctx.Done():
		t.Fatal("consumer did not resume after temporary failure")
	}
	<-done
}

func testValues(recipientID string) map[string]interface{} {
	return map[string]interface{}{
		"event": "message.created", "message_id": "a2e35e87-7560-4db8-b55a-57393f472914",
		"thread_id": "a6c6ecbf-4255-4b16-b4e0-009013da8d9b", "sender_id": "ea0d7404-28c7-4d28-812c-0f3c9a43a7b7",
		"recipient_id": recipientID, "seq": "7", "kind": "text", "content_format": "plaintext",
		"content": "hello", "created_at": "2026-09-28T00:00:00Z",
	}
}

func TestRedisPendingRecoveryIntegration(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("Redis test server unavailable: %v", err)
	}
	stream := "mini-hermes:gateway-test:" + time.Now().UTC().Format("20060102150405.000000000")
	group, name := "gateway-test", "gateway-1"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		_ = client.Del(cleanupCtx, stream).Err()
	})
	if err := client.XGroupCreateMkStream(ctx, stream, group, "0").Err(); err != nil {
		t.Fatal(err)
	}
	hub, endpoint := testServer(t)
	bobID := "84ac2d9f-2684-4da7-bf27-1c487207808a"
	bob := dial(t, endpoint, "Bearer "+testJWT(t, "gateway-test-secret", time.Hour, bobID))
	awaitClients(t, hub, bobID, 1)
	_, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: testValues(bobID)}).Result()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the previous process dying after XREADGROUP, before XACK.
	previous, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: name, Streams: []string{stream, ">"}, Count: 1, Block: -1,
	}).Result()
	if err != nil || len(previous) != 1 || len(previous[0].Messages) != 1 {
		t.Fatalf("previous read: %v %v", previous, err)
	}
	consumer := NewConsumer(client, hub, stream, group, name, log.New(io.Discard, "", 0))
	if err := consumer.recoverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if got := readMessage(t, bob); got.MessageID != testValues(bobID)["message_id"] {
		t.Fatalf("recovered wrong message: %+v", got)
	}
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil || pending.Count != 0 {
		t.Fatalf("pending after recovery: %+v %v", pending, err)
	}

	// Two stream entries for one external message ID must both be forwarded.
	for range 2 {
		if _, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: testValues(bobID)}).Result(); err != nil {
			t.Fatal(err)
		}
	}
	streams, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: name, Streams: []string{stream, ">"}, Count: 2, Block: -1,
	}).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.handleStreams(ctx, streams); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if got := readMessage(t, bob); got.MessageID != testValues(bobID)["message_id"] {
			t.Fatalf("duplicate event lost: %+v", got)
		}
	}
	_ = bob.CloseNow()
	awaitClients(t, hub, bobID, 0)
	if _, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: testValues(bobID)}).Result(); err != nil {
		t.Fatal(err)
	}
	streams, err = client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: group, Consumer: name, Streams: []string{stream, ">"}, Count: 1, Block: -1,
	}).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := consumer.handleStreams(ctx, streams); err != nil {
		t.Fatal(err)
	}
	pending, err = client.XPending(ctx, stream, group).Result()
	if err != nil || pending.Count != 0 {
		t.Fatalf("offline Bob should still be ACKed: %+v %v", pending, err)
	}
}

func TestRedisTicketToWebSocketIntegration(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	stream := "mini-hermes:gateway-ticket-test:" + uuid.NewString()
	manager, err := token.NewJWT("test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tickets := wsticket.NewService(wsticket.NewRedisRepository(client), 30*time.Second)
	hub := NewHub()
	server := httptest.NewServer(NewHandler(hub, manager, tickets, nil))
	defer server.Close()
	bobID, aliceID := uuid.NewString(), uuid.NewString()
	ticket, err := tickets.Issue(ctx, bobID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "?ticket=" + ticket
	conn, response, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		t.Fatalf("ticket handshake: %v response=%v", err, response)
	}
	defer conn.CloseNow()
	awaitClients(t, hub, bobID, 1)
	consumer := NewConsumer(client, hub, stream, "gateway-ticket-test", "gateway-1", log.New(io.Discard, "", 0))
	done := make(chan struct{})
	go func() { consumer.Run(ctx); close(done) }()
	defer func() {
		cancel()
		<-done
		_ = client.Del(context.Background(), stream).Err()
	}()
	event := messageevent.MessageCreated{
		MessageID: uuid.NewString(), ThreadID: uuid.NewString(), SenderID: aliceID,
		RecipientID: bobID, Seq: 1, Kind: "text", ContentFormat: "plaintext",
		Content: "Alice to Bob", CreatedAt: time.Now().UTC(),
	}
	if err := publisher.NewRedis(client, stream).Publish(ctx, event); err != nil {
		t.Fatal(err)
	}
	got := readMessage(t, conn)
	if got.MessageID != event.MessageID || got.RecipientID != bobID || got.Content != event.Content {
		t.Fatalf("wrong event delivered: %+v", got)
	}
}
