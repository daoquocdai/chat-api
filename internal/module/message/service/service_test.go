package service_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoquocdai/chat-api/internal/e2ee"
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
	version                   func(context.Context, string, int64) (int64, error)
	load                      func(context.Context, string, int64) ([]string, error)
	versionCalls, memberCalls int
	send                      func(context.Context, string, int64, string, string, string) (model.Message, bool, error)
	list                      func(context.Context, string, int64, *int64, int) (model.Page, error)
	sendCalls                 int
	listCalls                 int
}

func (r *fakeMessageRepository) MembershipVersionAtSequence(ctx context.Context, thread string, seq int64) (int64, error) {
	r.versionCalls++
	if r.version != nil {
		return r.version(ctx, thread, seq)
	}
	return 1, nil
}

func (r *fakeMessageRepository) ListMemberIDsAtSequence(ctx context.Context, thread string, seq int64) ([]string, error) {
	r.memberCalls++
	if r.load != nil {
		return r.load(ctx, thread, seq)
	}
	return []string{actorExternalID, "22222222-2222-4222-8222-222222222222"}, nil
}

func (r *fakeMessageRepository) Send(
	ctx context.Context,
	threadID string,
	senderID int64,
	messageID, contentFormat, content string,
) (model.Message, bool, error) {
	r.sendCalls++
	return r.send(ctx, threadID, senderID, messageID, contentFormat, content)
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

func TestSendGroupPlaintext(t *testing.T) {
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
				ID:               9,
				ExternalID:       messageExternalID,
				ThreadExternalID: threadExternalID,
				ThreadKind:       "group",
				SenderExternalID: actorExternalID,
				Seq:              1,
				Kind:             "text",
				ContentFormat:    "plaintext",
				Content:          tt.wantContent,
				CreatedAt:        time.Date(2026, time.September, 28, 9, 0, 0, 0, time.UTC),
			}
			repository := &fakeMessageRepository{send: func(
				gotCtx context.Context,
				threadID string,
				senderID int64,
				messageID, contentFormat, content string,
			) (model.Message, bool, error) {
				if gotCtx != ctx || threadID != threadExternalID || senderID != 11 || messageID != messageExternalID || contentFormat != "plaintext" || content != tt.wantContent {
					t.Fatalf("unexpected Send arguments: %q %d %q %q", threadID, senderID, messageID, content)
				}
				return wantMessage, true, tt.repositoryError
			}}

			publisher := &fakePublisher{}
			got, created, err := service.New(repository, users, publisher, nil, time.Second).Send(
				ctx,
				actorExternalID,
				tt.threadID,
				tt.messageID,
				"",
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
			if tt.wantError == nil && (!reflect.DeepEqual(got, wantMessage) || !created) {
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

			page, err := service.New(repository, users, &fakePublisher{}, nil, time.Second).List(ctx, actorExternalID, tt.threadID, tt.beforeSeq, tt.limit)
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
		ID:               9,
		ExternalID:       messageExternalID,
		ThreadExternalID: threadExternalID,
		ThreadKind:       "group",
		SenderExternalID: actorExternalID,
		Seq:              7,
		Kind:             "text",
		ContentFormat:    "plaintext",
		Content:          "hello",
		CreatedAt:        createdAt,
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
			repository := &fakeMessageRepository{
				version: func(context.Context, string, int64) (int64, error) { order = append(order, "version"); return 1, nil },
				load: func(context.Context, string, int64) ([]string, error) {
					order = append(order, "members")
					return []string{actorExternalID, "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"}, nil
				},
				send: func(
					context.Context,
					string,
					int64,
					string,
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
					ThreadKind:    wantMessage.ThreadKind,
					SenderID:      wantMessage.SenderExternalID,
					RecipientIDs:  []string{actorExternalID, "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"},
					Seq:           wantMessage.Seq,
					Kind:          wantMessage.Kind,
					ContentFormat: wantMessage.ContentFormat,
					Content:       wantMessage.Content,
					CreatedAt:     wantMessage.CreatedAt,
				}
				if !reflect.DeepEqual(gotEvent, wantEvent) {
					t.Fatalf("event = %+v, want %+v", gotEvent, wantEvent)
				}
				return tt.publisherError
			}}

			got, created, err := service.New(repository, users, publisher, nil, time.Second).Send(
				ctx,
				actorExternalID,
				threadExternalID,
				messageExternalID,
				"plaintext",
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
			if tt.repositoryError == nil && !reflect.DeepEqual(got, wantMessage) {
				t.Fatalf("message = %+v, want persisted message %+v", got, wantMessage)
			}
			if tt.wantPublishCalls == 1 && strings.Join(order, ",") != "repository,version,members,publisher" {
				t.Fatalf("call order = %v, want commit before recipients and publish", order)
			}
		})
	}
}

func TestRetryAfterPublishFailurePublishesAgainWithoutIncrementingSequence(t *testing.T) {
	ctx := context.Background()
	redisError := errors.New("redis unavailable")
	message := model.Message{
		ExternalID:       messageExternalID,
		ThreadExternalID: threadExternalID,
		ThreadKind:       "group",
		SenderExternalID: actorExternalID,
		Seq:              4,
		Kind:             "text",
		ContentFormat:    "plaintext",
		Content:          "retry me",
		CreatedAt:        time.Now(),
	}
	repositoryCalls := 0
	repository := &fakeMessageRepository{send: func(
		context.Context,
		string,
		int64,
		string,
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
	messageService := service.New(repository, users, publisher, nil, time.Second)

	first, firstCreated, firstError := messageService.Send(
		ctx, actorExternalID, threadExternalID, messageExternalID, "", "retry me",
	)
	second, secondCreated, secondError := messageService.Send(
		ctx, actorExternalID, threadExternalID, messageExternalID, "plaintext", "retry me",
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

func e2eeContent(t *testing.T) string {
	t.Helper()
	sessionKey := [32]byte{1, 2, 3, 4, 5}
	envelope, err := e2ee.SealMessage(e2ee.EpochMessageContext{
		ThreadID: threadExternalID, EpochID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		MessageID: messageExternalID, SenderID: actorExternalID,
		RecipientID: "22222222-2222-4222-8222-222222222222",
	}, sessionKey, []byte("encrypted test message"))
	if err != nil {
		t.Fatal(err)
	}
	content, err := e2ee.EncodeMessageEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	// Server must preserve opaque bytes, including valid surrounding whitespace.
	return "\n" + content + strings.Repeat(" ", 1200) + "\n"
}

func TestSendE2EEValidationAndOpaquePublishing(t *testing.T) {
	content := e2eeContent(t)
	tests := []struct {
		name, format, content string
		repoErr, wantErr      error
	}{
		{name: "opaque bytes exceed plaintext rune limit", format: "e2ee_v2", content: content},
		{name: "legacy v1 format rejected", format: "e2ee_v1", content: content, wantErr: model.ErrInvalidContentFormat},
		{name: "unknown format", format: "E2EE_V2", content: content, wantErr: model.ErrInvalidContentFormat},
		{name: "plaintext in e2ee format", format: "e2ee_v2", content: "hello", wantErr: model.ErrInvalidEnvelope},
		{name: "missing fields", format: "e2ee_v2", content: "{}", wantErr: model.ErrInvalidEnvelope},
		{name: "extra field", format: "e2ee_v2", content: strings.Replace(content, "{", `{"extra":true,`, 1), wantErr: model.ErrInvalidEnvelope},
		{name: "duplicate field", format: "e2ee_v2", content: strings.Replace(content, "{", `{"version":2,`, 1), wantErr: model.ErrInvalidEnvelope},
		{name: "oversized envelope", format: "e2ee_v2", content: content + strings.Repeat(" ", 8192), wantErr: model.ErrInvalidEnvelope},
		{name: "invalid UTF8", format: "e2ee_v2", content: content + "\xff", wantErr: model.ErrInvalidEnvelope},
		{name: "trailing JSON", format: "e2ee_v2", content: content + "{}", wantErr: model.ErrInvalidEnvelope},
		{name: "wrong thread kind preserved", format: "e2ee_v2", content: content, repoErr: model.ErrContentFormatConflict, wantErr: model.ErrContentFormatConflict},
		{name: "wrong header preserved", format: "e2ee_v2", content: content, repoErr: model.ErrInvalidEnvelopeHeader, wantErr: model.ErrInvalidEnvelopeHeader},
		{name: "inactive membership preserved", format: "e2ee_v2", content: content, repoErr: threadmodel.ErrNotParticipant, wantErr: threadmodel.ErrNotParticipant},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &fakeUserFinder{get: func(context.Context, string) (usermodel.User, error) { return usermodel.User{ID: 11}, nil }}
			repo := &fakeMessageRepository{send: func(_ context.Context, thread string, actor int64, id, format, raw string) (model.Message, bool, error) {
				if thread != threadExternalID || actor != 11 || id != messageExternalID || format != tt.format || raw != tt.content {
					t.Fatal("envelope bytes/format/context changed")
				}
				return model.Message{ExternalID: id, ThreadExternalID: thread, ThreadKind: "direct", SenderExternalID: actorExternalID, Seq: 1, Kind: "text", ContentFormat: format, Content: raw}, true, tt.repoErr
			}}
			publisher := &fakePublisher{}
			got, _, err := service.New(repo, users, publisher, nil, time.Second).Send(context.Background(), actorExternalID, threadExternalID, messageExternalID, tt.format, tt.content)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && tt.repoErr == nil && (repo.sendCalls != 0 || users.calls != 0) {
				t.Fatal("invalid request reached repository/user lookup")
			}
			if tt.wantErr != nil && publisher.calls != 0 {
				t.Fatal("rejected message was published")
			}
			if err == nil && (got.Content != content || got.ContentFormat != "e2ee_v2" || publisher.calls != 1 || publisher.events[0].Content != content || publisher.events[0].ContentFormat != "e2ee_v2") {
				t.Fatal("response/event did not preserve opaque content/format")
			}
		})
	}
}

func TestE2EERetryAfterPublishFailureKeepsExactPayload(t *testing.T) {
	content := e2eeContent(t)
	var saved *model.Message
	repo := &fakeMessageRepository{send: func(_ context.Context, thread string, actor int64, id, format, raw string) (model.Message, bool, error) {
		if saved != nil {
			if saved.Content != raw || saved.ContentFormat != format {
				return model.Message{}, false, model.ErrMessageIDConflict
			}
			return *saved, false, nil
		}
		saved = &model.Message{ID: 9, ExternalID: id, ThreadExternalID: thread, ThreadKind: "direct", SenderExternalID: actorExternalID, Seq: 1, Kind: "text", ContentFormat: format, Content: raw}
		return *saved, true, nil
	}}
	users := &fakeUserFinder{get: func(context.Context, string) (usermodel.User, error) { return usermodel.User{ID: 11}, nil }}
	publisher := &fakePublisher{}
	publisher.publish = func(context.Context, messageevent.MessageCreated) error {
		if publisher.calls == 1 {
			return errors.New("publish unavailable")
		}
		return nil
	}
	svc := service.New(repo, users, publisher, nil, time.Second)
	first, created, err := svc.Send(context.Background(), actorExternalID, threadExternalID, messageExternalID, "e2ee_v2", content)
	if !created || !errors.Is(err, model.ErrEventPublishFailed) {
		t.Fatal("expected committed message with publish failure")
	}
	second, created, err := svc.Send(context.Background(), actorExternalID, threadExternalID, messageExternalID, "e2ee_v2", content)
	if created || err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("retry did not return saved message")
	}
	_, _, err = svc.Send(context.Background(), actorExternalID, threadExternalID, messageExternalID, "e2ee_v2", content+" ")
	if !errors.Is(err, model.ErrMessageIDConflict) || publisher.calls != 2 || publisher.events[0].Content != publisher.events[1].Content {
		t.Fatal("changed bytes were not rejected or retry changed event")
	}
}

type fakeMembershipCache struct {
	snapshots          map[int64][]string
	readErr, writeErr  error
	getCalls, putCalls int
}

func (c *fakeMembershipCache) Get(ctx context.Context, _ string, version int64) ([]string, error) {
	c.getCalls++
	if _, ok := ctx.Deadline(); !ok {
		panic("cache read has no deadline")
	}
	return c.snapshots[version], c.readErr
}

func TestPublishMessageSnapshots(t *testing.T) {
	peer := "22222222-2222-4222-8222-222222222222"
	dbErr, cacheErr := errors.New("snapshot query failed"), errors.New("cache unavailable")
	cases := []struct {
		name                                 string
		cached, loaded                       []string
		readErr, writeErr, dbErr, versionErr error
		kind                                 string
		version                              int64
		wantDB, wantPut, wantPublish         int
		wantRecipients                       []string
		wantError                            bool
	}{
		{name: "miss loads and fills", loaded: []string{actorExternalID, peer}, kind: "text", version: 1, wantDB: 1, wantPut: 1, wantPublish: 1, wantRecipients: []string{actorExternalID, peer}},
		{name: "hit avoids UUID query", cached: []string{actorExternalID, peer}, kind: "text", version: 1, wantPublish: 1, wantRecipients: []string{actorExternalID, peer}},
		{name: "read error ignores cache", cached: []string{actorExternalID}, loaded: []string{actorExternalID, peer}, readErr: cacheErr, kind: "text", version: 1, wantDB: 1, wantPut: 1, wantPublish: 1, wantRecipients: []string{actorExternalID, peer}},
		{name: "write error still publishes", loaded: []string{actorExternalID, peer}, writeErr: cacheErr, kind: "text", version: 1, wantDB: 1, wantPut: 1, wantPublish: 1, wantRecipients: []string{actorExternalID, peer}},
		{name: "snapshot error does not publish", dbErr: dbErr, kind: "text", version: 1, wantDB: 1, wantError: true},
		{name: "version error does not publish", versionErr: dbErr, kind: "text", wantError: true},
		{name: "empty snapshot does not publish", loaded: []string{}, kind: "text", version: 1, wantDB: 1, wantError: true},
		{name: "invalid version does not publish", kind: "text", version: 0, wantError: true},
		{name: "future version does not publish", kind: "text", version: 4, wantError: true},
		{name: "system includes actor", cached: []string{actorExternalID, peer}, kind: "system", version: 1, wantPublish: 1, wantRecipients: []string{actorExternalID, peer}},
		{name: "solo text includes actor devices", cached: []string{actorExternalID}, kind: "text", version: 1, wantPublish: 1, wantRecipients: []string{actorExternalID}},
	}
	for _, threadKind := range []string{"direct", "group"} {
		for _, tc := range cases {
			t.Run(threadKind+"/"+tc.name, func(t *testing.T) {
				cache := &fakeMembershipCache{snapshots: map[int64][]string{1: tc.cached}, readErr: tc.readErr, writeErr: tc.writeErr}
				repo := &fakeMessageRepository{
					version: func(ctx context.Context, thread string, seq int64) (int64, error) {
						if thread != threadExternalID || seq != 3 {
							t.Fatal("version must use saved thread/seq")
						}
						return tc.version, tc.versionErr
					},
					load: func(ctx context.Context, thread string, seq int64) ([]string, error) {
						if thread != threadExternalID || seq != 3 {
							t.Fatal("snapshot must use saved thread/seq")
						}
						return tc.loaded, tc.dbErr
					},
				}
				out := &fakePublisher{}
				svc := service.New(repo, nil, out, cache, time.Second)
				err := svc.PublishMessage(context.Background(), model.Message{ExternalID: messageExternalID, ThreadExternalID: threadExternalID, ThreadKind: threadKind, SenderExternalID: actorExternalID, Seq: 3, Kind: tc.kind})
				if (err != nil) != tc.wantError || repo.memberCalls != tc.wantDB || cache.putCalls != tc.wantPut || out.calls != tc.wantPublish {
					t.Fatalf("err=%v UUID/cache/publish=%d/%d/%d", err, repo.memberCalls, cache.putCalls, out.calls)
				}
				if tc.wantError {
					var saved *model.EventPublishError
					if !errors.As(err, &saved) || saved.MessageID != messageExternalID || saved.ThreadID != threadExternalID || saved.Seq != 3 {
						t.Fatal("lost committed identity")
					}
				}
				if tc.wantPublish == 1 && !reflect.DeepEqual(out.events[0].RecipientIDs, tc.wantRecipients) {
					t.Fatalf("recipients=%v want=%v", out.events[0].RecipientIDs, tc.wantRecipients)
				}
				if tc.readErr == nil && tc.cached != nil && !reflect.DeepEqual(cache.snapshots[1], tc.cached) {
					t.Fatal("publishing changed the shared membership snapshot")
				}
			})
		}
	}
}

func TestPublishMessageMembershipChangeAndOldRetry(t *testing.T) {
	bob, charlie := "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"
	cache := &fakeMembershipCache{snapshots: map[int64][]string{}}
	repo := &fakeMessageRepository{
		version: func(_ context.Context, _ string, seq int64) (int64, error) {
			if seq <= 5 {
				return 1, nil
			}
			return 6, nil
		},
		load: func(_ context.Context, _ string, seq int64) ([]string, error) {
			if seq <= 5 {
				return []string{actorExternalID, bob}, nil
			}
			return []string{actorExternalID, charlie}, nil
		},
	}
	out := &fakePublisher{}
	svc := service.New(repo, nil, out, cache, time.Second)
	steps := []struct {
		seq          int64
		sender, kind string
		want         []string
	}{
		{2, actorExternalID, "text", []string{actorExternalID, bob}},
		{3, bob, "text", []string{actorExternalID, bob}},
		{5, actorExternalID, "system", []string{actorExternalID, bob}},
		{7, actorExternalID, "text", []string{actorExternalID, charlie}},
		{2, actorExternalID, "text", []string{actorExternalID, bob}},
	}
	for _, step := range steps {
		err := svc.PublishMessage(context.Background(), model.Message{ThreadExternalID: threadExternalID, ThreadKind: "group", Seq: step.seq, SenderExternalID: step.sender, Kind: step.kind})
		if err != nil {
			t.Fatal(err)
		}
		if got := out.events[len(out.events)-1].RecipientIDs; !reflect.DeepEqual(got, step.want) {
			t.Fatalf("seq=%d recipients=%v want=%v", step.seq, got, step.want)
		}
	}
	if repo.memberCalls != 2 {
		t.Fatalf("UUID queries=%d want one per version", repo.memberCalls)
	}
	delete(cache.snapshots, 1)
	if err := svc.PublishMessage(context.Background(), model.Message{ThreadExternalID: threadExternalID, ThreadKind: "group", Seq: 2, SenderExternalID: actorExternalID, Kind: "text"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out.events[len(out.events)-1].RecipientIDs, []string{actorExternalID, bob}) || repo.memberCalls != 3 {
		t.Fatal("expired old snapshot used current membership")
	}
}

func TestPublishMessageAfterRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	checkContext := func(ctx context.Context) {
		t.Helper()
		if ctx.Err() != nil {
			t.Fatal("publish inherited request cancellation")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > time.Second {
			t.Fatal("publish timeout missing")
		}
	}
	repo := &fakeMessageRepository{version: func(ctx context.Context, _ string, _ int64) (int64, error) { checkContext(ctx); return 1, nil }}
	pub := &fakePublisher{publish: func(ctx context.Context, _ messageevent.MessageCreated) error {
		checkContext(ctx)
		return errors.New("XADD timed out")
	}}
	svc := service.New(repo, nil, pub, nil, time.Second)
	err := svc.PublishMessage(ctx, model.Message{ExternalID: messageExternalID, ThreadExternalID: threadExternalID, Seq: 4})
	var saved *model.EventPublishError
	if !errors.As(err, &saved) || saved.MessageID != messageExternalID || saved.Seq != 4 {
		t.Fatalf("lost saved identity: %v", err)
	}
}

func (c *fakeMembershipCache) Put(ctx context.Context, _ string, version int64, ids []string) error {
	c.putCalls++
	if _, ok := ctx.Deadline(); !ok {
		panic("cache write has no deadline")
	}
	if c.writeErr == nil {
		c.snapshots[version] = append([]string(nil), ids...)
	}
	return c.writeErr
}
