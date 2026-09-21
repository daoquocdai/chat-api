package repository

import (
	"context"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type PostgresRepository struct {
	queries *sqlc.Queries
}

func New(queries *sqlc.Queries) *PostgresRepository {
	return &PostgresRepository{
		queries: queries,
	}
}

func (r *PostgresRepository) Create(
	ctx context.Context,
	username string,
) (model.User, error) {
	user, err := r.queries.CreateUser(ctx, username)
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "users_username_key" {
			return model.User{}, model.ErrUsernameTaken
		}

		return model.User{}, err
	}

	return toModel(user), nil
}

func (r *PostgresRepository) GetByExternalID(
	ctx context.Context,
	externalID string,
) (model.User, error) {
	var id pgtype.UUID

	if err := id.Scan(externalID); err != nil {
		return model.User{}, model.ErrInvalidUserID
	}

	user, err := r.queries.GetUserByExternalID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, model.ErrUserNotFound
		}

		return model.User{}, err
	}

	return toModel(user), nil
}

func (r *PostgresRepository) List(ctx context.Context) ([]model.User, error) {
	users, err := r.queries.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]model.User, len(users))
	for i, user := range users {
		result[i] = toModel(user)
	}

	return result, nil
}

func toModel(user sqlc.User) model.User {
	return model.User{
		ID:         user.ID,
		ExternalID: user.ExternalID.String(),
		Username:   user.Username,
		CreatedAt:  user.CreatedAt.Time,
	}
}
