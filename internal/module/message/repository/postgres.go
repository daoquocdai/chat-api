package repository

import (
	"context"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	"github.com/jackc/pgx/v5"
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
	clientMessageID, content string,
) (model.Message, bool, error) {
	threadID, err := parseUUID(threadExternalID, model.ErrInvalidThreadID)
	if err != nil {
		return model.Message{}, false, err
	}
	clientID, err := parseUUID(clientMessageID, model.ErrInvalidClientMessageID)
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

		existing, err := queries.GetMessageByClientID(ctx, sqlc.GetMessageByClientIDParams{
			ThreadID:    threadInternalID,
			SenderID:    senderID,
			ClientMsgID: clientID,
		})
		if err == nil {
			result = messageFromValues(
				existing.ID, existing.ExternalID, existing.ThreadExternalID,
				existing.SenderExternalID, existing.Seq, existing.ClientMsgID,
				existing.Kind, existing.ContentFormat, existing.Content, existing.CreatedAt,
			)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		seq, err := queries.IncrementThreadSequence(ctx, threadInternalID)
		if err != nil {
			return err
		}
		message, err := queries.CreateThreadMessage(ctx, sqlc.CreateThreadMessageParams{
			ThreadID:    threadInternalID,
			SenderID:    senderID,
			Seq:         seq,
			ClientMsgID: clientID,
			Content:     content,
		})
		if err != nil {
			return err
		}

		created = true
		result = messageFromValues(
			message.ID, message.ExternalID, message.ThreadExternalID,
			message.SenderExternalID, message.Seq, message.ClientMsgID,
			message.Kind, message.ContentFormat, message.Content, message.CreatedAt,
		)
		return nil
	})
	if err != nil {
		return model.Message{}, false, err
	}

	return result, created, nil
}

func (r *PostgresRepository) List(
	ctx context.Context,
	threadExternalID string,
	userID int64,
) ([]model.Message, error) {
	threadID, err := parseUUID(threadExternalID, model.ErrInvalidThreadID)
	if err != nil {
		return nil, err
	}
	queries := sqlc.New(r.pool)
	if _, err := queries.GetActiveThreadAccess(ctx, sqlc.GetActiveThreadAccessParams{
		UserID:           userID,
		ThreadExternalID: threadID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, classifyThreadAccess(ctx, queries, threadID)
		}
		return nil, err
	}

	rows, err := queries.ListThreadMessages(ctx, sqlc.ListThreadMessagesParams{
		UserID:           userID,
		ThreadExternalID: threadID,
	})
	if err != nil {
		return nil, err
	}

	messages := make([]model.Message, len(rows))
	for i, row := range rows {
		messages[i] = messageFromValues(
			row.ID, row.ExternalID, row.ThreadExternalID,
			row.SenderExternalID, row.Seq, row.ClientMsgID,
			row.Kind, row.ContentFormat, row.Content, row.CreatedAt,
		)
	}
	return messages, nil
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
	clientMessageID pgtype.UUID,
	kind, contentFormat, content string,
	createdAt pgtype.Timestamptz,
) model.Message {
	return model.Message{
		ID:               id,
		ExternalID:       externalID.String(),
		ThreadExternalID: threadExternalID.String(),
		SenderExternalID: senderExternalID.String(),
		Seq:              seq,
		ClientMessageID:  clientMessageID.String(),
		Kind:             kind,
		ContentFormat:    contentFormat,
		Content:          content,
		CreatedAt:        createdAt.Time,
	}
}
