package integration_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	messageModel "github.com/daoquocdai/chat-api/internal/module/message/model"
	messageRepository "github.com/daoquocdai/chat-api/internal/module/message/repository"
	threadModel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	threadRepository "github.com/daoquocdai/chat-api/internal/module/thread/repository"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const finalSchema = `
CREATE TABLE users (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    username VARCHAR(50) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE threads (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    kind TEXT NOT NULL CHECK (kind IN ('direct', 'group')),
    name TEXT,
    created_by BIGINT NOT NULL REFERENCES users(id),
    encryption_mode TEXT NOT NULL DEFAULT 'plaintext' CHECK (encryption_mode IN ('plaintext', 'e2ee')),
    last_seq BIGINT NOT NULL DEFAULT 0 CHECK (last_seq >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT threads_kind_shape_check CHECK (
        (kind = 'direct' AND name IS NULL)
        OR (kind = 'group' AND name IS NOT NULL AND BTRIM(name) <> '')
    ),
    CHECK (kind <> 'group' OR encryption_mode = 'plaintext')
);

CREATE TABLE participants (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    thread_id BIGINT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id),
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    joined_seq BIGINT NOT NULL CHECK (joined_seq >= 1),
    left_seq BIGINT,
    last_read_seq BIGINT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at TIMESTAMPTZ,
    UNIQUE (thread_id, user_id, joined_seq),
    CHECK (last_read_seq >= joined_seq - 1),
    CHECK (left_seq IS NULL OR left_seq >= joined_seq - 1),
    CHECK ((left_seq IS NULL) = (left_at IS NULL)),
    CHECK (left_seq IS NULL OR last_read_seq <= left_seq)
);

CREATE UNIQUE INDEX participants_active_membership_key
    ON participants (thread_id, user_id) WHERE left_seq IS NULL;

CREATE TABLE messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    thread_id BIGINT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    sender_id BIGINT NOT NULL REFERENCES users(id),
    seq BIGINT NOT NULL CHECK (seq >= 1),
    kind TEXT NOT NULL DEFAULT 'text' CHECK (kind IN ('text', 'system')),
    content_format TEXT NOT NULL DEFAULT 'plaintext' CHECK (content_format IN ('plaintext', 'e2ee_v1')),
    content TEXT NOT NULL CHECK (content <> ''),
    metadata JSONB NOT NULL DEFAULT '{}'::JSONB CHECK (jsonb_typeof(metadata) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (thread_id, seq)
);
`

type testUser struct {
	id         int64
	externalID string
}

func TestPostgresDirectChatBehavior(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL concurrency/integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool := newIsolatedPool(t, ctx, databaseURL)
	threadRepo := threadRepository.New(pool)
	messageRepo := messageRepository.New(pool)

	alice := insertUser(t, ctx, pool, "11111111-1111-4111-8111-111111111111", "alice")
	bob := insertUser(t, ctx, pool, "22222222-2222-4222-8222-222222222222", "bob")
	mallory := insertUser(t, ctx, pool, "33333333-3333-4333-8333-333333333333", "mallory")

	t.Run("opposite concurrent requests create one direct thread", func(t *testing.T) {
		type result struct {
			thread  threadModel.Thread
			created bool
			err     error
		}

		start := make(chan struct{})
		results := make(chan result, 2)
		var wait sync.WaitGroup
		for _, pair := range [][2]int64{{alice.id, bob.id}, {bob.id, alice.id}} {
			wait.Add(1)
			go func(creatorID, peerID int64) {
				defer wait.Done()
				<-start
				thread, created, err := threadRepo.CreateOrGetDirect(ctx, creatorID, peerID)
				results <- result{thread: thread, created: created, err: err}
			}(pair[0], pair[1])
		}
		close(start)
		wait.Wait()
		close(results)

		var got []result
		createdCount := 0
		for result := range results {
			if result.err != nil {
				t.Fatalf("CreateOrGetDirect failed: %v", result.err)
			}
			if result.created {
				createdCount++
			}
			got = append(got, result)
		}
		if createdCount != 1 {
			t.Fatalf("created count = %d, want 1", createdCount)
		}
		if got[0].thread.ExternalID != got[1].thread.ExternalID {
			t.Fatalf("thread IDs differ: %q and %q", got[0].thread.ExternalID, got[1].thread.ExternalID)
		}

		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM threads", 1)
		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM participants", 2)
	})

	threads, err := threadRepo.ListByUser(ctx, alice.id)
	if err != nil || len(threads) != 1 {
		t.Fatalf("list Alice threads = (%+v, %v), want one thread", threads, err)
	}
	threadID := threads[0].ExternalID

	t.Run("retry and external ID conflicts preserve sequence", func(t *testing.T) {
		const firstMessageID = "aaaaaaaa-1111-4111-8111-111111111111"
		type result struct {
			message messageModel.Message
			created bool
			err     error
		}

		start := make(chan struct{})
		results := make(chan result, 2)
		var wait sync.WaitGroup
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				message, created, err := messageRepo.Send(ctx, threadID, alice.id, firstMessageID, "same content")
				results <- result{message: message, created: created, err: err}
			}()
		}
		close(start)
		wait.Wait()
		close(results)

		createdCount := 0
		for result := range results {
			if result.err != nil || result.message.ExternalID != firstMessageID || result.message.Seq != 1 ||
				result.message.RecipientExternalID != bob.externalID {
				t.Fatalf("retry result = (%+v, %v, %v)", result.message, result.created, result.err)
			}
			if result.created {
				createdCount++
			}
		}
		if createdCount != 1 {
			t.Fatalf("created count = %d, want 1", createdCount)
		}

		second, created, err := messageRepo.Send(
			ctx,
			threadID,
			alice.id,
			"bbbbbbbb-2222-4222-8222-222222222222",
			"same content",
		)
		if err != nil || !created || second.Seq != 2 {
			t.Fatalf("same content with new UUID = (%+v, %v, %v), want seq 2 created", second, created, err)
		}

		_, created, err = messageRepo.Send(ctx, threadID, alice.id, firstMessageID, "different content")
		if !errors.Is(err, messageModel.ErrMessageIDConflict) || created {
			t.Fatalf("changed retry = (created %v, error %v), want conflict", created, err)
		}
		_, _, err = messageRepo.Send(ctx, threadID, bob.id, firstMessageID, "same content")
		if !errors.Is(err, messageModel.ErrMessageIDConflict) {
			t.Fatalf("other sender reused UUID error = %v, want conflict", err)
		}

		assertCount(t, ctx, pool, "SELECT COUNT(*) FROM messages", 2)
		assertCount(t, ctx, pool, "SELECT last_seq FROM threads", 2)
	})

	for index, messageID := range []string{
		"cccccccc-3333-4333-8333-333333333333",
		"dddddddd-4444-4444-8444-444444444444",
		"eeeeeeee-5555-4555-8555-555555555555",
	} {
		message, created, err := messageRepo.Send(ctx, threadID, alice.id, messageID, fmt.Sprintf("message %d", index+3))
		if err != nil || !created || message.Seq != int64(index+3) {
			t.Fatalf("seed message %d = (%+v, %v, %v)", index+3, message, created, err)
		}
	}

	t.Run("third party cannot read send or mark read", func(t *testing.T) {
		_, err := messageRepo.List(ctx, threadID, mallory.id, nil, 2)
		if !errors.Is(err, threadModel.ErrNotParticipant) {
			t.Fatalf("Mallory list error = %v, want not participant", err)
		}
		_, _, err = messageRepo.Send(
			ctx,
			threadID,
			mallory.id,
			"ffffffff-6666-4666-8666-666666666666",
			"forbidden",
		)
		if !errors.Is(err, threadModel.ErrNotParticipant) {
			t.Fatalf("Mallory send error = %v, want not participant", err)
		}
		_, err = threadRepo.MarkRead(ctx, threadID, mallory.id, 1)
		if !errors.Is(err, threadModel.ErrNotParticipant) {
			t.Fatalf("Mallory mark-read error = %v, want not participant", err)
		}
	})

	t.Run("cursor and monotonic read marker remain correct", func(t *testing.T) {
		firstPage, err := messageRepo.List(ctx, threadID, alice.id, nil, 2)
		if err != nil {
			t.Fatalf("first page: %v", err)
		}
		if len(firstPage.Messages) != 2 || firstPage.Messages[0].Seq != 5 || firstPage.Messages[1].Seq != 4 ||
			firstPage.NextCursor == nil || *firstPage.NextCursor != 4 {
			t.Fatalf("first page = %+v, cursor %v; want [5,4], cursor 4", firstPage.Messages, firstPage.NextCursor)
		}

		secondPage, err := messageRepo.List(ctx, threadID, alice.id, firstPage.NextCursor, 2)
		if err != nil {
			t.Fatalf("second page: %v", err)
		}
		if len(secondPage.Messages) != 2 || secondPage.Messages[0].Seq != 3 || secondPage.Messages[1].Seq != 2 ||
			secondPage.NextCursor == nil || *secondPage.NextCursor != 2 {
			t.Fatalf("second page = %+v, cursor %v; want [3,2], cursor 2", secondPage.Messages, secondPage.NextCursor)
		}

		stored, err := threadRepo.MarkRead(ctx, threadID, bob.id, 5)
		if err != nil || stored != 5 {
			t.Fatalf("mark read 5 = (%d, %v)", stored, err)
		}
		stored, err = threadRepo.MarkRead(ctx, threadID, bob.id, 3)
		if err != nil || stored != 5 {
			t.Fatalf("older mark read = (%d, %v), want 5", stored, err)
		}

		bobThreads, err := threadRepo.ListByUser(ctx, bob.id)
		if err != nil || len(bobThreads) != 1 || bobThreads[0].LastReadSeq != 5 || bobThreads[0].UnreadCount != 0 {
			t.Fatalf("Bob thread summary = (%+v, %v)", bobThreads, err)
		}
	})
}

func newIsolatedPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()

	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create admin pool: %v", err)
	}
	if err := admin.Ping(ctx); err != nil {
		admin.Close()
		t.Fatalf("connect TEST_DATABASE_URL: %v", err)
	}

	schemaName := fmt.Sprintf("mini_hermes_test_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		admin.Close()
		t.Fatalf("create isolated schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		admin.Close()
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName
	config.MaxConns = 6
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Close()
		t.Fatalf("create isolated pool: %v", err)
	}
	if _, err := pool.Exec(ctx, finalSchema); err != nil {
		pool.Close()
		admin.Close()
		t.Fatalf("create final schema: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if !strings.HasPrefix(schemaName, "mini_hermes_test_") {
			t.Errorf("refusing to clean unexpected schema %q", schemaName)
			admin.Close()
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema %s: %v", schemaName, err)
		}
		admin.Close()
	})

	return pool
}

func insertUser(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	externalID, username string,
) testUser {
	t.Helper()
	var id int64
	if err := pool.QueryRow(
		ctx,
		"INSERT INTO users (external_id, username) VALUES ($1, $2) RETURNING id",
		externalID,
		username,
	).Scan(&id); err != nil {
		t.Fatalf("insert user %s: %v", username, err)
	}
	return testUser{id: id, externalID: externalID}
}

func assertCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, want int64) {
	t.Helper()
	var got int64
	if err := pool.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q = %d, want %d", query, got, want)
	}
}
