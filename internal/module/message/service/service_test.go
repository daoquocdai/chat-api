package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	messageevent "github.com/daoquocdai/chat-api/internal/module/message/event"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
	"github.com/daoquocdai/chat-api/internal/module/message/service"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
)

const (
	actorExternalID   = "11111111-1111-4111-8111-111111111111"
	threadExternalID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	messageExternalID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

type fakeMessageRepository struct {
	send      func(context.Context, string, int64, string, string) (model.Message, bool, error)
	list      func(context.Context, string, int64, *int64, int) (model.Page, error)
	sendCalls int
	listCalls int
}

func (r *fakeMessageRepository) Send(
	ctx context.Context,
	threadID string,
	senderID int64,
	messageID, content string,
) (model.Message, bool, error) {
	r.sendCalls++
	return r.send(ctx, threadID, senderID, messageID, content)
}

func (r *fakeMessageRepository) List(
	ctx context.Context,
	threadID string,
	userID int64,
	beforeSeq *int64,
	limit int,
) (model.Page, error) {
	r.listCalls++
	return r.list(ctx, threadID, userID, beforeSeq, limit)
}

type fakeUserFinder struct {
	get   func(context.Context, string) (usermodel.User, error)
	calls int
}

type fakePublisher struct {
	publish func(context.Context, messageevent.MessageCreated) error
	calls   int
	events  []messageevent.MessageCreated
}

func (p *fakePublisher) Publish(ctx context.Context, event messageevent.MessageCreated) error {
	p.calls++
	p.events = append(p.events, event)
	if p.publish == nil {
		return nil
	}
	return p.publish(ctx, event)
}

func (f *fakeUserFinder) GetByExternalID(ctx context.Context, externalID string) (usermodel.User, error) {
	f.calls++
	return f.get(ctx, externalID)
}

func TestSend(t *testing.T) {
	databaseError := errors.New("database unavailable")

	tests := []struct {
		name             string
		threadID         string
		messageID        string
		content          string
		userError        error
		repositoryError  error
		wantError        error
		wantContent      string
		wantUserCalls    int
		wantRepoCalls    int
		wantPublishCalls int
	}{
		{name: "sends normalized content", threadID: " " + threadExternalID + " ", messageID: " " + messageExternalID + " ", content: "  xin chào  ", wantContent: "xin chào", wantUserCalls: 1, wantRepoCalls: 1, wantPublishCalls: 1},
		{name: "thread ID required", threadID: " ", messageID: messageExternalID, content: "hello", wantError: model.ErrThreadIDRequired},
		{name: "message ID required", threadID: threadExternalID, messageID: " ", content: "hello", wantError: model.ErrMessageIDRequired},
		{name: "empty content", threadID: threadExternalID, messageID: messageExternalID, content: " \t", wantError: model.ErrInvalidContent},
		{name: "content too long", threadID: threadExternalID, messageID: messageExternalID, content: strings.Repeat("đ", 1001), wantError: model.ErrInvalidContent},
		{name: "content contains NUL", threadID: threadExternalID, messageID: messageExternalID, content: "hello\x00", wantError: model.ErrInvalidContent},
		{name: "actor lookup fails", threadID: threadExternalID, messageID: messageExternalID, content: "hello", userError: usermodel.ErrUserNotFound, wantError: usermodel.ErrUserNotFound, wantUserCalls: 1},
		{name: "conflict is preserved", threadID: threadExternalID, messageID: messageExternalID, content: "hello", repositoryError: model.ErrMessageIDConflict, wantError: model.ErrMessageIDConflict, wantContent: "hello", wantUserCalls: 1, wantRepoCalls: 1},
		{name: "third party is rejected", threadID: threadExternalID, messageID: messageExternalID, content: "hello", repositoryError: threadmodel.ErrNotParticipant, wantError: threadmodel.ErrNotParticipant, wantContent: "hello", wantUserCalls: 1, wantRepoCalls: 1},
		{name: "database error", threadID: threadExternalID, messageID: messageExternalID, content: "hello", repositoryError: databaseError, wantError: databaseError, wantContent: "hello", wantUserCalls: 1, wantRepoCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			users := &fakeUserFinder{get: func(gotCtx context.Context, externalID string) (usermodel.User, error) {
				if gotCtx != ctx || externalID != actorExternalID {
					t.Fatalf("unexpected user lookup: %q", externalID)
				}
				if tt.userError != nil {
					return usermodel.User{}, tt.userError
				}
				return usermodel.User{ID: 11, ExternalID: actorExternalID}, nil
			}}
			wantMessage := model.Message{
				ID:                  9,
				ExternalID:          messageExternalID,
				ThreadExternalID:    threadExternalID,
				SenderExternalID:    actorExternalID,
				RecipientExternalID: "22222222-2222-4222-8222-222222222222",
				Seq:                 1,
				Kind:                "text",
				ContentFormat:       "plaintext",
				Content:             tt.wantContent,
				CreatedAt:           time.Date(2026, time.September, 28, 9, 0, 0, 0, time.UTC),
			}
			repository := &fakeMessageRepository{send: func(
				gotCtx context.Context,
				threadID string,
				senderID int64,
				messageID, content string,
			) (model.Message, bool, error) {
				if gotCtx != ctx || threadID != threadExternalID || senderID != 11 || messageID != messageExternalID || content != tt.wantContent {
					t.Fatalf("unexpected Send arguments: %q %d %q %q", threadID, senderID, messageID, content)
				}
				return wantMessage, true, tt.repositoryError
			}}

			publisher := &fakePublisher{}
			got, created, err := service.New(repository, users, publisher, time.Second).Send(
				ctx,
				actorExternalID,
				tt.threadID,
				tt.messageID,
				tt.content,
			)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if users.calls != tt.wantUserCalls || repository.sendCalls != tt.wantRepoCalls {
				t.Fatalf("calls = (users %d, repository %d), want (%d, %d)", users.calls, repository.sendCalls, tt.wantUserCalls, tt.wantRepoCalls)
			}
			if publisher.calls != tt.wantPublishCalls {
				t.Fatalf("publisher calls = %d, want %d", publisher.calls, tt.wantPublishCalls)
			}
			if tt.wantError == nil && (got != wantMessage || !created) {
				t.Fatalf("result = (%+v, %v), want (%+v, true)", got, created, wantMessage)
			}
		})
	}
}

func TestList(t *testing.T) {
	beforeFive := int64(5)
	zero := int64(0)

	tests := []struct {
		name          string
		threadID      string
		beforeSeq     *int64
		limit         int
		repositoryErr error
		wantError     error
		wantRepoCalls int
	}{
		{name: "forwards cursor", threadID: " " + threadExternalID + " ", beforeSeq: &beforeFive, limit: 2, wantRepoCalls: 1},
		{name: "empty thread ID", threadID: " ", limit: 2, wantError: model.ErrThreadIDRequired},
		{name: "zero cursor", threadID: threadExternalID, beforeSeq: &zero, limit: 2, wantError: model.ErrInvalidBeforeSeq},
		{name: "zero limit", threadID: threadExternalID, limit: 0, wantError: model.ErrInvalidLimit},
		{name: "limit too large", threadID: threadExternalID, limit: 101, wantError: model.ErrInvalidLimit},
		{name: "third party is rejected", threadID: threadExternalID, limit: 2, repositoryErr: threadmodel.ErrNotParticipant, wantError: threadmodel.ErrNotParticipant, wantRepoCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			users := &fakeUserFinder{get: func(context.Context, string) (usermodel.User, error) {
				return usermodel.User{ID: 11, ExternalID: actorExternalID}, nil
			}}
			repository := &fakeMessageRepository{list: func(
				gotCtx context.Context,
				threadID string,
				userID int64,
				beforeSeq *int64,
				limit int,
			) (model.Page, error) {
				if gotCtx != ctx || threadID != threadExternalID || userID != 11 || limit != tt.limit {
					t.Fatalf("unexpected List arguments: %q %d %d", threadID, userID, limit)
				}
				if (beforeSeq == nil) != (tt.beforeSeq == nil) || (beforeSeq != nil && *beforeSeq != *tt.beforeSeq) {
					t.Fatalf("beforeSeq = %v, want %v", beforeSeq, tt.beforeSeq)
				}
				return model.Page{}, tt.repositoryErr
			}}

			page, err := service.New(repository, users, &fakePublisher{}, time.Second).List(ctx, actorExternalID, tt.threadID, tt.beforeSeq, tt.limit)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if repository.listCalls != tt.wantRepoCalls {
				t.Fatalf("repository calls = %d, want %d", repository.listCalls, tt.wantRepoCalls)
			}
			if tt.wantError == nil && page.Messages == nil {
				t.Fatal("nil messages were not normalized to an empty slice")
			}
		})
	}
}

func TestSendPersistenceAndPublishing(t *testing.T) {
	databaseError := errors.New("transaction failed")
	redisError := errors.New("redis unavailable")
	createdAt := time.Date(2026, time.September, 28, 10, 30, 0, 123, time.FixedZone("ICT", 7*60*60))
	wantMessage := model.Message{
		ID:                  9,
		ExternalID:          messageExternalID,
		ThreadExternalID:    threadExternalID,
		SenderExternalID:    actorExternalID,
		RecipientExternalID: "22222222-2222-4222-8222-222222222222",
		Seq:                 7,
		Kind:                "text",
		ContentFormat:       "plaintext",
		Content:             "hello",
		CreatedAt:           createdAt,
	}

	tests := []struct {
		name             string
		repositoryError  error
		publisherError   error
		wantError        error
		wantCreated      bool
		wantPublishCalls int
	}{
		{name: "save then publish", wantCreated: true, wantPublishCalls: 1},
		{name: "transaction error does not publish", repositoryError: databaseError, wantError: databaseError},
		{name: "publish error reports saved message", publisherError: redisError, wantError: model.ErrEventPublishFailed, wantCreated: true, wantPublishCalls: 1},
		{name: "UUID conflict does not publish", repositoryError: model.ErrMessageIDConflict, wantError: model.ErrMessageIDConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			order := []string{}
			users := &fakeUserFinder{get: func(context.Context, string) (usermodel.User, error) {
				return usermodel.User{ID: 11, ExternalID: actorExternalID}, nil
			}}
			repository := &fakeMessageRepository{send: func(
				context.Context,
				string,
				int64,
				string,
				string,
			) (model.Message, bool, error) {
				order = append(order, "repository")
				if tt.repositoryError != nil {
					return model.Message{}, false, tt.repositoryError
				}
				return wantMessage, true, nil
			}}
			publisher := &fakePublisher{publish: func(gotCtx context.Context, gotEvent messageevent.MessageCreated) error {
				order = append(order, "publisher")
				deadline, ok := gotCtx.Deadline()
				if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > time.Second {
					t.Fatalf("publisher context deadline = %v, want active deadline within one second", deadline)
				}
				wantEvent := messageevent.MessageCreated{
					MessageID:     wantMessage.ExternalID,
					ThreadID:      wantMessage.ThreadExternalID,
					SenderID:      wantMessage.SenderExternalID,
					RecipientID:   wantMessage.RecipientExternalID,
					Seq:           wantMessage.Seq,
					Kind:          wantMessage.Kind,
					ContentFormat: wantMessage.ContentFormat,
					Content:       wantMessage.Content,
					CreatedAt:     wantMessage.CreatedAt,
				}
				if gotEvent != wantEvent {
					t.Fatalf("event = %+v, want %+v", gotEvent, wantEvent)
				}
				return tt.publisherError
			}}

			got, created, err := service.New(repository, users, publisher, time.Second).Send(
				ctx,
				actorExternalID,
				threadExternalID,
				messageExternalID,
				"hello",
			)
			if !errors.Is(err, tt.wantError) {
				t.Fatalf("error = %v, want %v", err, tt.wantError)
			}
			if created != tt.wantCreated {
				t.Fatalf("created = %v, want %v", created, tt.wantCreated)
			}
			if publisher.calls != tt.wantPublishCalls {
				t.Fatalf("publisher calls = %d, want %d", publisher.calls, tt.wantPublishCalls)
			}
			if tt.repositoryError == nil && got != wantMessage {
				t.Fatalf("message = %+v, want persisted message %+v", got, wantMessage)
			}
			if tt.wantPublishCalls == 1 && strings.Join(order, ",") != "repository,publisher" {
				t.Fatalf("call order = %v, want repository then publisher", order)
			}
		})
	}
}

func TestRetryAfterPublishFailurePublishesAgainWithoutIncrementingSequence(t *testing.T) {
	ctx := context.Background()
	redisError := errors.New("redis unavailable")
	message := model.Message{
		ExternalID:          messageExternalID,
		ThreadExternalID:    threadExternalID,
		SenderExternalID:    actorExternalID,
		RecipientExternalID: "22222222-2222-4222-8222-222222222222",
		Seq:                 4,
		Kind:                "text",
		ContentFormat:       "plaintext",
		Content:             "retry me",
		CreatedAt:           time.Now(),
	}
	repositoryCalls := 0
	repository := &fakeMessageRepository{send: func(
		context.Context,
		string,
		int64,
		string,
		string,
	) (model.Message, bool, error) {
		repositoryCalls++
		return message, repositoryCalls == 1, nil
	}}
	users := &fakeUserFinder{get: func(context.Context, string) (usermodel.User, error) {
		return usermodel.User{ID: 11, ExternalID: actorExternalID}, nil
	}}
	publisher := &fakePublisher{}
	publisher.publish = func(context.Context, messageevent.MessageCreated) error {
		if publisher.calls == 1 {
			return redisError
		}
		return nil
	}
	messageService := service.New(repository, users, publisher, time.Second)

	first, firstCreated, firstError := messageService.Send(
		ctx, actorExternalID, threadExternalID, messageExternalID, "retry me",
	)
	second, secondCreated, secondError := messageService.Send(
		ctx, actorExternalID, threadExternalID, messageExternalID, "retry me",
	)

	if !errors.Is(firstError, model.ErrEventPublishFailed) || secondError != nil {
		t.Fatalf("send errors = (%v, %v), want publish failure then success", firstError, secondError)
	}
	if !firstCreated || secondCreated {
		t.Fatalf("created flags = (%v, %v), want (true, false)", firstCreated, secondCreated)
	}
	if first.Seq != 4 || second.Seq != 4 || first.ExternalID != second.ExternalID {
		t.Fatalf("retry messages = (%+v, %+v), want same UUID and seq", first, second)
	}
	if publisher.calls != 2 {
		t.Fatalf("publisher calls = %d, want 2", publisher.calls)
	}
}
