package publisher

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/redis/go-redis/v9"
)

func TestRedisPublisherIntegration(t *testing.T) {
	address := os.Getenv("TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set; skipping Redis Streams integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("connect TEST_REDIS_ADDR: %v", err)
	}

	stream := fmt.Sprintf("mini-hermes:test:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		defer client.Close()
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cleanupCancel()
		if err := client.Del(cleanupContext, stream).Err(); err != nil {
			t.Errorf("delete isolated test stream: %v", err)
		}
	})

	event := messageevent.MessageCreated{
		MessageID:     "11111111-1111-4111-8111-111111111111",
		ThreadID:      "22222222-2222-4222-8222-222222222222",
		SenderID:      "33333333-3333-4333-8333-333333333333",
		RecipientID:   "44444444-4444-4444-8444-444444444444",
		Seq:           1,
		Kind:          "text",
		ContentFormat: "plaintext",
		Content:       "integration",
		CreatedAt:     time.Date(2026, time.September, 28, 0, 0, 0, 0, time.UTC),
	}
	if err := NewRedis(client, stream).Publish(ctx, event); err != nil {
		t.Fatalf("XADD message.created: %v", err)
	}

	entries, err := client.XRangeN(ctx, stream, "-", "+", 2).Result()
	if err != nil {
		t.Fatalf("XRANGE test stream: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("stream entry count = %d, want 1", len(entries))
	}
	if entries[0].Values["event"] != messageevent.MessageCreatedType ||
		entries[0].Values["recipient_id"] != event.RecipientID ||
		entries[0].Values["message_id"] != event.MessageID {
		t.Fatalf("stream values = %+v", entries[0].Values)
	}
}
