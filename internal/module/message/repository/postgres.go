package repository

import (
	"context"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/module/message/model"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresRepository struct {
	queries *sqlc.Queries
}

func New(queries *sqlc.Queries) *PostgresRepository {
	return &PostgresRepository{queries: queries}
}

func (r *PostgresRepository) Create(
	ctx context.Context,
	senderID int64,
	receiverID int64,
	content string,
) (model.Message, error) {
	message, err := r.queries.CreateMessage(ctx, sqlc.CreateMessageParams{
		SenderID:   senderID,
		ReceiverID: receiverID,
		Content:    content,
	})
	if err != nil {
		return model.Message{}, err
	}

	return toModel(
		message.ID,
		message.ExternalID,
		message.SenderExternalID,
		message.ReceiverExternalID,
		message.Content,
		message.CreatedAt,
	), nil
}

func (r *PostgresRepository) ListBetween(
	ctx context.Context,
	userOneID int64,
	userTwoID int64,
) ([]model.Message, error) {
	messages, err := r.queries.ListMessagesBetween(
		ctx,
		sqlc.ListMessagesBetweenParams{
			UserOneID: userOneID,
			UserTwoID: userTwoID,
		},
	)
	if err != nil {
		return nil, err
	}

	result := make([]model.Message, len(messages))
	for i, message := range messages {
		result[i] = toModel(
			message.ID,
			message.ExternalID,
			message.SenderExternalID,
			message.ReceiverExternalID,
			message.Content,
			message.CreatedAt,
		)
	}

	return result, nil
}

func toModel(
	id int64,
	externalID pgtype.UUID,
	senderExternalID pgtype.UUID,
	receiverExternalID pgtype.UUID,
	content string,
	createdAt pgtype.Timestamptz,
) model.Message {
	return model.Message{
		ID:                 id,
		ExternalID:         externalID.String(),
		SenderExternalID:   senderExternalID.String(),
		ReceiverExternalID: receiverExternalID.String(),
		Content:            content,
		CreatedAt:          createdAt.Time,
	}
}
