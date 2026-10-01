package repository

import (
	"context"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Send(
	ctx context.Context,
	threadExternalID string,
	senderID int64,
	messageID, content string,
) (model.Message, bool, error) {
	threadID, err := parseUUID(threadExternalID, model.ErrInvalidThreadID)
	if err != nil {
		return model.Message{}, false, err
	}
	messageExternalID, err := parseUUID(messageID, model.ErrInvalidMessageID)
	if err != nil {
		return model.Message{}, false, err
	}

	var result model.Message
	created := false
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)
		threadInternalID, err := queries.LockThreadForParticipant(ctx, sqlc.LockThreadForParticipantParams{
			UserID:           senderID,
			ThreadExternalID: threadID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return classifyThreadAccess(ctx, queries, threadID)
			}
			return err
		}
		// Recheck after obtaining the thread lock: membership may have changed while waiting.
		if _, err := queries.GetActiveParticipant(ctx, sqlc.GetActiveParticipantParams{
			ThreadID: threadInternalID, UserID: senderID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return threadmodel.ErrNotParticipant
			}
			return err
		}

		existing, err := queries.GetMessageByExternalID(ctx, messageExternalID)
		if err == nil {
			if existing.ThreadID != threadInternalID || existing.SenderID != senderID ||
				existing.Kind != "text" || existing.ContentFormat != "plaintext" ||
				existing.Content != content {
				return model.ErrMessageIDConflict
			}
			result = messageFromValues(
				existing.ID, existing.ExternalID, existing.ThreadExternalID,
				existing.SenderExternalID, existing.Seq, existing.ThreadKind, existing.Kind,
				existing.ContentFormat, existing.Content, existing.CreatedAt,
			)
			return setRecipients(ctx, queries, threadInternalID, senderID, &result)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		seq, err := queries.IncrementThreadSequence(ctx, threadInternalID)
		if err != nil {
			return err
		}
		message, err := queries.CreateThreadMessage(ctx, sqlc.CreateThreadMessageParams{
			MessageExternalID: messageExternalID,
			ThreadID:          threadInternalID,
			SenderID:          senderID,
			Seq:               seq,
			Content:           content,
		})
		if err != nil {
			return err
		}

		created = true
		result = messageFromValues(
			message.ID, message.ExternalID, message.ThreadExternalID,
			message.SenderExternalID, message.Seq, message.ThreadKind, message.Kind,
			message.ContentFormat, message.Content, message.CreatedAt,
		)
		return setRecipients(ctx, queries, threadInternalID, senderID, &result)
	})
	if err != nil {
		if isExternalIDUniqueViolation(err) {
			return model.Message{}, false, model.ErrMessageIDConflict
		}
		return model.Message{}, false, err
	}

	return result, created, nil
}

func (r *PostgresRepository) List(
	ctx context.Context,
	threadExternalID string,
	userID int64,
	beforeSeq *int64,
	limit int,
) (model.Page, error) {
	threadID, err := parseUUID(threadExternalID, model.ErrInvalidThreadID)
	if err != nil {
		return model.Page{}, err
	}
	queries := sqlc.New(r.pool)
	if _, err := queries.GetThreadHistoryAccess(ctx, sqlc.GetThreadHistoryAccessParams{
		UserID:           userID,
		ThreadExternalID: threadID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Page{}, classifyThreadAccess(ctx, queries, threadID)
		}
		return model.Page{}, err
	}

	before := pgtype.Int8{}
	if beforeSeq != nil {
		before = pgtype.Int8{Int64: *beforeSeq, Valid: true}
	}
	rows, err := queries.ListThreadMessagesPage(ctx, sqlc.ListThreadMessagesPageParams{
		UserID:           userID,
		ThreadExternalID: threadID,
		BeforeSeq:        before,
		PageSize:         int32(limit),
	})
	if err != nil {
		return model.Page{}, err
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	messages := make([]model.Message, len(rows))
	for i, row := range rows {
		messages[i] = messageFromValues(
			row.ID, row.ExternalID, row.ThreadExternalID,
			row.SenderExternalID, row.Seq, row.ThreadKind, row.Kind,
			row.ContentFormat, row.Content, row.CreatedAt,
		)
	}

	var nextCursor *int64
	if hasMore {
		cursor := messages[len(messages)-1].Seq
		nextCursor = &cursor
	}

	return model.Page{Messages: messages, NextCursor: nextCursor}, nil
}

func classifyThreadAccess(ctx context.Context, queries *sqlc.Queries, threadID pgtype.UUID) error {
	exists, err := queries.ThreadExistsByExternalID(ctx, threadID)
	if err != nil {
		return err
	}
	if exists {
		return threadmodel.ErrNotParticipant
	}
	return threadmodel.ErrThreadNotFound
}

func parseUUID(value string, invalidError error) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, invalidError
	}
	return id, nil
}

func messageFromValues(
	id int64,
	externalID, threadExternalID, senderExternalID pgtype.UUID,
	seq int64,
	threadKind, kind, contentFormat, content string,
	createdAt pgtype.Timestamptz,
) model.Message {
	return model.Message{
		ID:               id,
		ExternalID:       externalID.String(),
		ThreadExternalID: threadExternalID.String(),
		ThreadKind:       threadKind,
		SenderExternalID: senderExternalID.String(),
		Seq:              seq,
		Kind:             kind,
		ContentFormat:    contentFormat,
		Content:          content,
		CreatedAt:        createdAt.Time,
	}
}

func isExternalIDUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		postgresError.Code == "23505" &&
		postgresError.ConstraintName == "messages_external_id_key"
}

func setRecipients(ctx context.Context, queries *sqlc.Queries, threadID, senderID int64, message *model.Message) error {
	recipients, err := queries.ListMessageRecipients(ctx, sqlc.ListMessageRecipientsParams{
		ThreadID: threadID, Seq: message.Seq, SenderID: senderID, IncludeSender: message.Kind == "system",
	})
	if err != nil {
		return err
	}
	message.RecipientIDs = make([]string, len(recipients))
	for i, id := range recipients {
		message.RecipientIDs[i] = id.String()
	}
	return nil
}
