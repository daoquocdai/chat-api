package publisher

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/redis/go-redis/v9"
)

type xAdder interface {
	XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd
}

type Redis struct {
	client xAdder
	stream string
}

func NewRedis(client xAdder, stream string) *Redis {
	return &Redis{client: client, stream: stream}
}

func (p *Redis) Publish(ctx context.Context, event messageevent.MessageCreated) error {
	recipients, err := json.Marshal(event.RecipientIDs)
	if err != nil {
		return err
	}
	return p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.stream,
		Values: map[string]any{
			"event":          messageevent.MessageCreatedType,
			"message_id":     event.MessageID,
			"thread_id":      event.ThreadID,
			"thread_kind":    event.ThreadKind,
			"sender_id":      event.SenderID,
			"recipient_ids":  string(recipients),
			"seq":            strconv.FormatInt(event.Seq, 10),
			"kind":           event.Kind,
			"content_format": event.ContentFormat,
			"content":        event.Content,
			"created_at":     event.CreatedAt.UTC().Format(time.RFC3339Nano),
		},
	}).Err()
}
