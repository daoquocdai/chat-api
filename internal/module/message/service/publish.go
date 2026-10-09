package service

import (
	"context"
	"fmt"
	"log"
	"time"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
)

const cacheOperationTimeout = 100 * time.Millisecond

type MembershipCache interface {
	Get(context.Context, string, int64) ([]string, error)
	Put(context.Context, string, int64, []string) error
}

// PublishMessage is shared by text and group operations, after their transaction commits.
func (s *Service) PublishMessage(ctx context.Context, message model.Message) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.publishTimeout)
	defer cancel()
	recipients, err := s.messageRecipients(ctx, message)
	if err == nil {
		event := messageevent.FromMessage(message, recipients)
		err = s.publisher.Publish(ctx, event)
	}
	if err != nil {
		return &model.EventPublishError{
			MessageID: message.ExternalID, ThreadID: message.ThreadExternalID,
			SenderID: message.SenderExternalID, Seq: message.Seq, Cause: err,
		}
	}
	return nil
}

func (s *Service) messageRecipients(ctx context.Context, message model.Message) ([]string, error) {
	// Membership boundaries after this seq cannot change its historical snapshot.
	version, err := s.repository.MembershipVersionAtSequence(ctx, message.ThreadExternalID, message.Seq)
	if err != nil {
		return nil, fmt.Errorf("load membership version: %w", err)
	}
	if version < 1 || version > message.Seq {
		return nil, fmt.Errorf("invalid membership version")
	}
	var members []string
	if s.cache != nil {
		cacheCtx, cancel := context.WithTimeout(ctx, cacheOperationTimeout)
		members, err = s.cache.Get(cacheCtx, message.ThreadExternalID, version)
		cancel()
		if err != nil {
			log.Printf("membership cache read failed thread_id=%s: %v", message.ThreadExternalID, err)
		}
	}
	if err != nil || members == nil {
		members, err = s.repository.ListMemberIDsAtSequence(ctx, message.ThreadExternalID, message.Seq)
		if err != nil {
			return nil, fmt.Errorf("load membership snapshot: %w", err)
		}
		if len(members) == 0 {
			return nil, fmt.Errorf("empty membership snapshot")
		}
		if s.cache != nil {
			cacheCtx, cancel := context.WithTimeout(ctx, cacheOperationTimeout)
			err = s.cache.Put(cacheCtx, message.ThreadExternalID, version, members)
			cancel()
			if err != nil {
				log.Printf("membership cache write failed thread_id=%s: %v", message.ThreadExternalID, err)
			}
		}
	}
	// Every device of both parties receives the event, including the sender's
	// other tabs. Clients merge the HTTP response and event by message UUID.
	return members, nil
}
