package repository

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/e2ee"
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
	encryptionMode *string,
) (model.Thread, bool, error) {
	lowID, highID := creatorID, peerID
	if lowID > highID {
		lowID, highID = highID, lowID
	}

	var result model.Thread
	created := false
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)
		lockedUsers, err := queries.LockUsersForDirectThread(ctx, sqlc.LockUsersForDirectThreadParams{
			UserLowID:  lowID,
			UserHighID: highID,
		})
		if err != nil {
			return err
		}
		if len(lockedUsers) != 2 {
			return fmt.Errorf("lock direct thread users: expected 2 users, got %d", len(lockedUsers))
		}

		var thread sqlc.CreateDirectThreadRow
		existing, err := queries.GetDirectThreadByParticipants(
			ctx,
			sqlc.GetDirectThreadByParticipantsParams{UserLowID: lowID, UserHighID: highID},
		)
		if err == nil {
			if encryptionMode != nil && *encryptionMode != existing.EncryptionMode {
				return model.ErrEncryptionModeConflict
			}
			thread = sqlc.CreateDirectThreadRow{
				ID:             existing.ID,
				ExternalID:     existing.ExternalID,
				Kind:           existing.Kind,
				EncryptionMode: existing.EncryptionMode,
				LastSeq:        existing.LastSeq,
				CreatedAt:      existing.CreatedAt,
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		} else {
			mode := "plaintext"
			if encryptionMode != nil {
				mode = *encryptionMode
			}
			if mode == "e2ee" {
				for _, user := range lockedUsers {
					spks, err := queries.GetE2EESignedPrekeys(ctx, user.ID)
					if err != nil {
						return err
					}
					if len(spks) != 1 {
						return model.ErrE2EEBundleRequired
					}
					if err := e2ee.VerifyBundle(e2ee.Bundle{UserID: user.ExternalID.String(),
						IdentityPublicKey: base64.StdEncoding.EncodeToString(user.IdentityPublicKey),
						SignedPrekey:      e2ee.SignedPrekey{KeyID: spks[0].KeyID, PublicKey: base64.StdEncoding.EncodeToString(spks[0].PublicKey), Signature: base64.StdEncoding.EncodeToString(spks[0].Signature)},
					}); err != nil {
						return model.ErrE2EEBundleRequired
					}
				}
			}
			thread, err = queries.CreateDirectThread(ctx, sqlc.CreateDirectThreadParams{CreatedBy: creatorID, EncryptionMode: mode})
			if err != nil {
				return err
			}
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
			created = true
		}

		summary, err := queries.GetThreadSummaryForUser(ctx, sqlc.GetThreadSummaryForUserParams{
			UserID:           creatorID,
			ThreadExternalID: thread.ExternalID,
		})
		if err != nil {
			return err
		}

		result = threadFromRow(summary)
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
		threads[i] = threadFromRow(sqlc.GetThreadSummaryForUserRow(row))
	}

	return threads, nil
}

func threadFromRow(row sqlc.GetThreadSummaryForUserRow) model.Thread {
	thread := model.Thread{
		ID: row.ID, ExternalID: row.ExternalID.String(), Kind: row.Kind,
		EncryptionMode: row.EncryptionMode,
		Name:           row.Name, Role: row.Role, MemberCount: row.MemberCount,
		LastSeq: row.LastSeq, JoinedSeq: row.JoinedSeq, LastReadSeq: row.LastReadSeq,
		PeerLastReadSeq: row.PeerLastReadSeq, UnreadCount: row.UnreadCount,
		CreatedAt: row.CreatedAt.Time,
	}
	if row.Kind == "direct" {
		thread.Peer = model.Peer{ExternalID: row.PeerExternalID.String(), Username: row.PeerUsername}
	}
	if row.LastMessageSeq.Valid {
		thread.LastMessage = &model.LastMessage{
			Seq:              row.LastMessageSeq.Int64,
			SenderExternalID: row.LastMessageSenderExternalID.String(),
			Content:          row.LastMessageContent.String, CreatedAt: row.LastMessageCreatedAt.Time,
		}
	}
	return thread
}

func (r *PostgresRepository) MarkRead(ctx context.Context, threadExternalID string, userID, lastReadSeq int64) (int64, error) {
	threadID, err := parseThreadUUID(threadExternalID)
	if err != nil {
		return 0, err
	}
	var stored int64
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		thread, err := q.LockThreadByExternalID(ctx, threadID)
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrThreadNotFound
		}
		if err != nil {
			return err
		}
		if _, err := q.GetActiveParticipant(ctx, sqlc.GetActiveParticipantParams{ThreadID: thread.ID, UserID: userID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return model.ErrNotParticipant
			}
			return err
		}
		stored, err = q.MarkThreadRead(ctx, sqlc.MarkThreadReadParams{LastReadSeq: lastReadSeq, UserID: userID, ThreadExternalID: threadID})
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrInvalidReadSequence
		}
		return err
	})
	return stored, err
}

func parseThreadUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, model.ErrInvalidThreadID
	}
	return id, nil
}
