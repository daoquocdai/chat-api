package repository

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/e2ee"
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
	messageID, contentFormat, content string,
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
		thread, err := queries.LockThreadForParticipant(ctx, sqlc.LockThreadForParticipantParams{
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
			ThreadID: thread.ID, UserID: senderID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return threadmodel.ErrNotParticipant
			}
			return err
		}

		existing, err := queries.GetMessageByExternalID(ctx, messageExternalID)
		if err == nil {
			if existing.ThreadID != thread.ID || existing.SenderID != senderID ||
				existing.Kind != "text" || existing.ContentFormat != contentFormat ||
				existing.Content != content {
				return model.ErrMessageIDConflict
			}
			result = messageFromValues(
				existing.ID, existing.ExternalID, existing.ThreadExternalID,
				existing.SenderExternalID, existing.Seq, existing.ThreadKind, existing.Kind,
				existing.ContentFormat, existing.Content, existing.CreatedAt,
			)
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		expectedFormat := "plaintext"
		if thread.Kind == "direct" && thread.EncryptionMode == "e2ee" {
			expectedFormat = "e2ee_v1"
		}
		if contentFormat != expectedFormat {
			return model.ErrContentFormatConflict
		}
		if contentFormat == "e2ee_v1" {
			if err := validateEnvelopeHeader(ctx, queries, thread.ID, senderID, content); err != nil {
				return err
			}
		}

		seq, err := queries.IncrementThreadSequence(ctx, thread.ID)
		if err != nil {
			return err
		}
		message, err := queries.CreateThreadMessage(ctx, sqlc.CreateThreadMessageParams{
			MessageExternalID: messageExternalID,
			ThreadID:          thread.ID,
			SenderID:          senderID,
			Seq:               seq,
			ContentFormat:     contentFormat,
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
		return nil
	})
	if err != nil {
		if isExternalIDUniqueViolation(err) {
			return model.Message{}, false, model.ErrMessageIDConflict
		}
		return model.Message{}, false, err
	}

	return result, created, nil
}

// The server checks public metadata only. Claimed OPKs have already been deleted;
// neither their presence nor successful AEAD decryption is a prerequisite here.
func validateEnvelopeHeader(ctx context.Context, queries *sqlc.Queries, threadID, senderID int64, content string) error {
	envelope, err := e2ee.ParseEnvelope(content)
	if err != nil {
		return model.ErrInvalidEnvelope
	}
	participants, err := queries.GetE2EEMessageParticipants(ctx, threadID)
	if err != nil {
		return err
	}
	if len(participants) != 2 {
		return model.ErrInvalidEnvelopeHeader
	}
	var recipientID int64
	var senderMatches bool
	for _, participant := range participants {
		if participant.ID == senderID {
			senderMatches = base64.StdEncoding.EncodeToString(participant.IdentityPublicKey) == envelope.SenderIdentityKey
		} else if participant.ExternalID.String() == envelope.RecipientID {
			recipientID = participant.ID
		}
	}
	if !senderMatches || recipientID == 0 {
		return model.ErrInvalidEnvelopeHeader
	}
	prekeys, err := queries.GetE2EESignedPrekeys(ctx, recipientID)
	if err != nil {
		return err
	}
	if len(prekeys) != 1 || prekeys[0].KeyID != envelope.SignedPrekeyID {
		return model.ErrInvalidEnvelopeHeader
	}
	return nil
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

// MembershipVersionAtSequence resolves the immutable snapshot key after commit.
func (r *PostgresRepository) MembershipVersionAtSequence(ctx context.Context, threadExternalID string, seq int64) (int64, error) {
	id, err := parseUUID(threadExternalID, model.ErrInvalidThreadID)
	if err != nil {
		return 0, err
	}
	return sqlc.New(r.pool).GetMembershipVersion(ctx, sqlc.GetMembershipVersionParams{ThreadExternalID: id, Seq: seq})
}

// ListMemberIDsAtSequence is only used by the publishing service after commit.
// Historical intervals make this safe even if membership changes before this query.
func (r *PostgresRepository) ListMemberIDsAtSequence(ctx context.Context, threadExternalID string, seq int64) ([]string, error) {
	id, err := parseUUID(threadExternalID, model.ErrInvalidThreadID)
	if err != nil {
		return nil, err
	}
	ids, err := sqlc.New(r.pool).ListMemberIDsAtSequence(ctx, sqlc.ListMemberIDsAtSequenceParams{ThreadExternalID: id, Seq: seq})
	if err != nil {
		return nil, err
	}
	members := make([]string, len(ids))
	for i, id := range ids {
		members[i] = id.String()
	}
	return members, nil
}
