package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/redis/go-redis/v9"
)

const DefaultGroup = "ws-gateway"
const DefaultConsumer = "gateway-1" // Stable across restarts; only one gateway instance is supported.

type Consumer struct {
	client   redis.Cmdable
	hub      *Hub
	stream   string
	group    string
	name     string
	logger   *log.Logger
	readWait time.Duration
}

func NewConsumer(client redis.Cmdable, hub *Hub, stream, group, name string, logger *log.Logger) *Consumer {
	return &Consumer{client: client, hub: hub, stream: stream, group: group, name: name, logger: logger, readWait: time.Second}
}

func (c *Consumer) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := c.consume(ctx); err != nil && ctx.Err() == nil {
			c.logger.Printf("Redis stream consumer error: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (c *Consumer) consume(ctx context.Context) error {
	if err := c.client.XGroupCreateMkStream(ctx, c.stream, c.group, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create group: %w", err)
	}
	// XREADGROUP with ID 0 replays this stable consumer's own pending entries.
	// The one-instance constraint means there are no other consumer PELs to claim.
	if err := c.recoverPending(ctx); err != nil {
		return err
	}
	for ctx.Err() == nil {
		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: c.group, Consumer: c.name, Streams: []string{c.stream, ">"},
			Count: 100, Block: c.readWait,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read new entries: %w", err)
		}
		if err := c.handleStreams(ctx, streams); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (c *Consumer) recoverPending(ctx context.Context) error {
	for ctx.Err() == nil {
		streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: c.group, Consumer: c.name, Streams: []string{c.stream, "0"}, Count: 100, Block: -1,
		}).Result()
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recover pending entries: %w", err)
		}
		if len(streams) == 0 || len(streams[0].Messages) == 0 {
			return nil
		}
		if err := c.handleStreams(ctx, streams); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func (c *Consumer) handleStreams(ctx context.Context, streams []redis.XStream) error {
	for _, stream := range streams {
		for _, entry := range stream.Messages {
			if err := ctx.Err(); err != nil {
				return err // Leave unprocessed entries pending for the next start.
			}
			event, err := decode(entry.Values)
			if err != nil {
				// A poison entry cannot be delivered; avoid a permanent pending loop.
				c.logger.Printf("discard invalid stream entry id=%s: %v", entry.ID, err)
			} else if err := c.hub.Dispatch(event); err != nil {
				return fmt.Errorf("queue entry id=%s: %w", entry.ID, err)
			}
			if err := c.client.XAck(ctx, c.stream, c.group, entry.ID).Err(); err != nil {
				return fmt.Errorf("ack entry id=%s: %w", entry.ID, err)
			}
		}
	}
	return nil
}

func decode(values map[string]interface{}) (messageevent.MessageCreated, error) {
	get := func(key string) (string, error) {
		value, ok := values[key].(string)
		if !ok || value == "" {
			return "", fmt.Errorf("missing %s", key)
		}
		return value, nil
	}
	typeName, err := get("event")
	if err != nil {
		return messageevent.MessageCreated{}, err
	}
	if typeName != messageevent.MessageCreatedType {
		return messageevent.MessageCreated{}, fmt.Errorf("unsupported event type")
	}
	var event messageevent.MessageCreated
	for _, field := range []struct {
		key  string
		dest *string
	}{
		{"message_id", &event.MessageID}, {"thread_id", &event.ThreadID},
		{"sender_id", &event.SenderID},
		{"kind", &event.Kind}, {"content_format", &event.ContentFormat}, {"content", &event.Content},
	} {
		// Empty content is allowed; all other fields are required.
		if field.key == "content" {
			value, ok := values[field.key].(string)
			if !ok {
				return messageevent.MessageCreated{}, fmt.Errorf("missing content")
			}
			*field.dest = value
		} else {
			*field.dest, err = get(field.key)
			if err != nil {
				return messageevent.MessageCreated{}, err
			}
		}
	}
	if text, ok := values["recipient_ids"].(string); ok {
		if err := json.Unmarshal([]byte(text), &event.RecipientIDs); err != nil || event.RecipientIDs == nil {
			return messageevent.MessageCreated{}, fmt.Errorf("invalid recipient_ids")
		}
	} else {
		// Read existing entries already stored before the recipient-list contract.
		recipient, err := get("recipient_id")
		if err != nil {
			return messageevent.MessageCreated{}, err
		}
		event.RecipientIDs = []string{recipient}
	}
	event.ThreadKind, _ = values["thread_kind"].(string)
	if event.ThreadKind == "" {
		event.ThreadKind = "direct"
	}
	seqText, err := get("seq")
	if err != nil {
		return messageevent.MessageCreated{}, err
	}
	event.Seq, err = strconv.ParseInt(seqText, 10, 64)
	if err != nil || event.Seq <= 0 {
		return messageevent.MessageCreated{}, fmt.Errorf("invalid seq")
	}
	createdText, err := get("created_at")
	if err != nil {
		return messageevent.MessageCreated{}, err
	}
	event.CreatedAt, err = time.Parse(time.RFC3339Nano, createdText)
	if err != nil {
		return messageevent.MessageCreated{}, fmt.Errorf("invalid created_at")
	}
	return event, nil
}
