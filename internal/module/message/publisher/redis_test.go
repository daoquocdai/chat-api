package publisher

import (
	"context"
	"errors"
	"testing"
	"time"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/redis/go-redis/v9"
)

type fakeXAdder struct {
	args  *redis.XAddArgs
	err   error
	calls int
}

func (f *fakeXAdder) XAdd(_ context.Context, args *redis.XAddArgs) *redis.StringCmd {
	f.calls++
	f.args = args
	return redis.NewStringResult("1-0", f.err)
}

func TestRedisPublishUsesMessageCreatedContract(t *testing.T) {
	createdAt := time.Date(2026, time.September, 28, 12, 34, 56, 789, time.FixedZone("ICT", 7*60*60))
	event := messageevent.MessageCreated{
		MessageID:     "11111111-1111-4111-8111-111111111111",
		ThreadID:      "22222222-2222-4222-8222-222222222222",
		SenderID:      "33333333-3333-4333-8333-333333333333",
		RecipientID:   "44444444-4444-4444-8444-444444444444",
		Seq:           17,
		Kind:          "text",
		ContentFormat: "plaintext",
		Content:       "hello",
		CreatedAt:     createdAt,
	}
	client := &fakeXAdder{}

	if err := NewRedis(client, "mini-hermes:events").Publish(context.Background(), event); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("XADD calls = %d, want 1", client.calls)
	}
	if client.args.Stream != "mini-hermes:events" {
		t.Fatalf("stream = %q, want mini-hermes:events", client.args.Stream)
	}

	values, ok := client.args.Values.(map[string]any)
	if !ok {
		t.Fatalf("XADD values type = %T, want map[string]any", client.args.Values)
	}
	want := map[string]any{
		"event":          "message.created",
		"message_id":     event.MessageID,
		"thread_id":      event.ThreadID,
		"sender_id":      event.SenderID,
		"recipient_id":   event.RecipientID,
		"seq":            "17",
		"kind":           event.Kind,
		"content_format": event.ContentFormat,
		"content":        event.Content,
		"created_at":     "2026-09-28T05:34:56.000000789Z",
	}
	if len(values) != len(want) {
		t.Fatalf("field count = %d, want %d: %+v", len(values), len(want), values)
	}
	for key, wantValue := range want {
		if got := values[key]; got != wantValue {
			t.Fatalf("field %q = %#v, want %#v", key, got, wantValue)
		}
	}
}

func TestRedisPublishReturnsXAddError(t *testing.T) {
	redisError := errors.New("redis unavailable")
	client := &fakeXAdder{err: redisError}

	err := NewRedis(client, "mini-hermes:events").Publish(
		context.Background(),
		messageevent.MessageCreated{},
	)
	if !errors.Is(err, redisError) {
		t.Fatalf("error = %v, want %v", err, redisError)
	}
}
