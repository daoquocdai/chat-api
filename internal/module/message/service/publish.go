package service

import (
	"context"
	"fmt"
	"log"
	"time"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
)

type MembershipRepository interface {
	ListMemberIDsAtSequence(context.Context, string, int64) ([]string, error)
}

const cacheOperationTimeout = 100 * time.Millisecond

type MembershipCache interface {
	Get(context.Context, string, int64) ([]string, error)
	Put(context.Context, string, int64, []string) error
}

// CachedPublisher resolves group recipients once per membership version, then publishes
// a self-contained event. The gateway needs neither this cache nor PostgreSQL.
type CachedPublisher struct {
	repository MembershipRepository
	cache      MembershipCache
	publisher  Publisher
}

func NewCachedPublisher(repository MembershipRepository, cache MembershipCache, publisher Publisher) *CachedPublisher {
	return &CachedPublisher{repository: repository, cache: cache, publisher: publisher}
}

func (p *CachedPublisher) Publish(ctx context.Context, event messageevent.MessageCreated) error {
	if event.ThreadKind == "group" {
		if event.MembershipVersion < 1 || event.MembershipVersion > event.Seq {
			return fmt.Errorf("invalid membership version")
		}
		// Cache work has a small separate budget; a slow cache must leave time for DB fallback/XADD.
		cacheCtx, cancel := context.WithTimeout(ctx, cacheOperationTimeout)
		members, err := p.cache.Get(cacheCtx, event.ThreadID, event.MembershipVersion)
		cancel()
		if err != nil {
			log.Printf("membership cache read failed thread_id=%s: %v", event.ThreadID, err)
		}
		if err != nil || members == nil {
			members, err = p.repository.ListMemberIDsAtSequence(ctx, event.ThreadID, event.Seq)
			if err != nil {
				return fmt.Errorf("load membership snapshot: %w", err)
			}
			if len(members) == 0 {
				return fmt.Errorf("empty membership snapshot")
			}
			cacheCtx, cancel := context.WithTimeout(ctx, cacheOperationTimeout)
			err = p.cache.Put(cacheCtx, event.ThreadID, event.MembershipVersion, members)
			cancel()
			if err != nil {
				log.Printf("membership cache write failed thread_id=%s: %v", event.ThreadID, err)
			}
		}
		// Cache includes the sender so every sender can reuse the same snapshot.
		event.RecipientIDs = make([]string, 0, len(members))
		for _, id := range members {
			if event.Kind == "system" || id != event.SenderID {
				event.RecipientIDs = append(event.RecipientIDs, id)
			}
		}
	}
	return p.publisher.Publish(ctx, event)
}

// PublishMessage is the single post-commit timeout/error path for text and system messages.
func PublishMessage(ctx context.Context, publisher Publisher, timeout time.Duration, message model.Message) error {
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	if err := publisher.Publish(publishCtx, messageevent.FromMessage(message)); err != nil {
		return &model.EventPublishError{
			MessageID: message.ExternalID, ThreadID: message.ThreadExternalID,
			SenderID: message.SenderExternalID, Seq: message.Seq, Cause: err,
		}
	}
	return nil
}
