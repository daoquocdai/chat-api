package repository

import (
	"context"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/module/thread/model"
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

func (r *PostgresRepository) CreateOrGetDirect(
	ctx context.Context,
	creatorID, peerID int64,
) (model.Thread, bool, error) {
	lowID, highID := creatorID, peerID
	if lowID > highID {
		lowID, highID = highID, lowID
	}

	var result model.Thread
	created := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)
		createParams := sqlc.CreateDirectThreadParams{
			CreatedBy:        creatorID,
			DirectUserLowID:  pgtype.Int8{Int64: lowID, Valid: true},
			DirectUserHighID: pgtype.Int8{Int64: highID, Valid: true},
		}

		thread, err := queries.CreateDirectThread(ctx, createParams)
		if err == nil {
			created = true
			if err := queries.CreateParticipant(ctx, sqlc.CreateParticipantParams{
				ThreadID: thread.ID,
				UserID:   lowID,
			}); err != nil {
				return err
			}
			if err := queries.CreateParticipant(ctx, sqlc.CreateParticipantParams{
				ThreadID: thread.ID,
				UserID:   highID,
			}); err != nil {
				return err
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := queries.GetDirectThreadByPair(ctx, sqlc.GetDirectThreadByPairParams{
				DirectUserLowID:  createParams.DirectUserLowID,
				DirectUserHighID: createParams.DirectUserHighID,
			})
			if getErr != nil {
				return getErr
			}
			thread = sqlc.CreateDirectThreadRow{
				ID:         existing.ID,
				ExternalID: existing.ExternalID,
				Kind:       existing.Kind,
				LastSeq:    existing.LastSeq,
				CreatedAt:  existing.CreatedAt,
			}
		} else {
			return err
		}

		summary, err := queries.GetThreadSummaryForUser(ctx, sqlc.GetThreadSummaryForUserParams{
			UserID:           creatorID,
			ThreadExternalID: thread.ExternalID,
		})
		if err != nil {
			return err
		}

		result = threadFromValues(
			summary.ID,
			summary.ExternalID,
			summary.Kind,
			summary.PeerExternalID,
			summary.PeerUsername,
			summary.LastSeq,
			summary.LastReadSeq,
			summary.PeerLastReadSeq,
			summary.UnreadCount,
			summary.LastMessageSeq,
			summary.LastMessageSenderExternalID,
			summary.LastMessageContent,
			summary.LastMessageCreatedAt,
			summary.CreatedAt,
		)
		return nil
	})
	if err != nil {
		return model.Thread{}, false, err
	}

	return result, created, nil
}

func (r *PostgresRepository) ListByUser(ctx context.Context, userID int64) ([]model.Thread, error) {
	rows, err := sqlc.New(r.pool).ListThreadsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	threads := make([]model.Thread, len(rows))
	for i, row := range rows {
		threads[i] = threadFromValues(
			row.ID,
			row.ExternalID,
			row.Kind,
			row.PeerExternalID,
			row.PeerUsername,
			row.LastSeq,
			row.LastReadSeq,
			row.PeerLastReadSeq,
			row.UnreadCount,
			row.LastMessageSeq,
			row.LastMessageSenderExternalID,
			row.LastMessageContent,
			row.LastMessageCreatedAt,
			row.CreatedAt,
		)
	}

	return threads, nil
}

func threadFromValues(
	id int64,
	externalID pgtype.UUID,
	kind string,
	peerExternalID pgtype.UUID,
	peerUsername string,
	lastSeq int64,
	lastReadSeq int64,
	peerLastReadSeq int64,
	unreadCount int64,
	lastMessageSeq pgtype.Int8,
	lastMessageSenderExternalID pgtype.UUID,
	lastMessageContent pgtype.Text,
	lastMessageCreatedAt pgtype.Timestamptz,
	createdAt pgtype.Timestamptz,
) model.Thread {
	thread := model.Thread{
		ID:              id,
		ExternalID:      externalID.String(),
		Kind:            kind,
		Peer:            model.Peer{ExternalID: peerExternalID.String(), Username: peerUsername},
		LastSeq:         lastSeq,
		LastReadSeq:     lastReadSeq,
		PeerLastReadSeq: peerLastReadSeq,
		UnreadCount:     unreadCount,
		CreatedAt:       createdAt.Time,
	}

	if lastMessageSeq.Valid {
		thread.LastMessage = &model.LastMessage{
			Seq:              lastMessageSeq.Int64,
			SenderExternalID: lastMessageSenderExternalID.String(),
			Content:          lastMessageContent.String,
			CreatedAt:        lastMessageCreatedAt.Time,
		}
	}

	return thread
}

func (r *PostgresRepository) MarkRead(
	ctx context.Context,
	threadExternalID string,
	userID, lastReadSeq int64,
) (int64, error) {
	threadID, err := parseThreadUUID(threadExternalID)
	if err != nil {
		return 0, err
	}
	queries := sqlc.New(r.pool)
	stored, err := queries.MarkThreadRead(ctx, sqlc.MarkThreadReadParams{
		LastReadSeq:      lastReadSeq,
		UserID:           userID,
		ThreadExternalID: threadID,
	})
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}

	if _, accessErr := queries.GetThreadReadBounds(ctx, sqlc.GetThreadReadBoundsParams{
		UserID:           userID,
		ThreadExternalID: threadID,
	}); accessErr == nil {
		return 0, model.ErrInvalidReadSequence
	} else if !errors.Is(accessErr, pgx.ErrNoRows) {
		return 0, accessErr
	}

	exists, err := queries.ThreadExistsByExternalID(ctx, threadID)
	if err != nil {
		return 0, err
	}
	if exists {
		return 0, model.ErrNotParticipant
	}
	return 0, model.ErrThreadNotFound
}

func parseThreadUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, model.ErrInvalidThreadID
	}
	return id, nil
}
