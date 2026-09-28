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
	return &PostgresRepository{queries: queries}
}

func (r *PostgresRepository) CreateWithPassword(
	ctx context.Context,
	username, passwordHash string,
) (model.User, error) {
	user, err := r.queries.CreateUserWithPassword(ctx, sqlc.CreateUserWithPasswordParams{
		Username:     username,
		PasswordHash: passwordHash,
	})
	if err != nil {
		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" &&
			pgErr.ConstraintName == "users_username_key" {
			return model.User{}, model.ErrUsernameTaken
		}

		return model.User{}, err
	}

	return toModel(user.ID, user.ExternalID, user.Username, user.CreatedAt), nil
}

func (r *PostgresRepository) GetCredentialsByUsername(
	ctx context.Context,
	username string,
) (model.Credentials, error) {
	user, err := r.queries.GetUserCredentialsByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Credentials{}, model.ErrUserNotFound
		}

		return model.Credentials{}, err
	}

	return model.Credentials{
		User:         toModel(user.ID, user.ExternalID, user.Username, user.CreatedAt),
		PasswordHash: user.PasswordHash,
	}, nil
}

func (r *PostgresRepository) GetByExternalID(ctx context.Context, externalID string) (model.User, error) {
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

	return toModel(user.ID, user.ExternalID, user.Username, user.CreatedAt), nil
}

func (r *PostgresRepository) List(ctx context.Context) ([]model.User, error) {
	users, err := r.queries.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]model.User, len(users))
	for i, user := range users {
		result[i] = toModel(user.ID, user.ExternalID, user.Username, user.CreatedAt)
	}

	return result, nil
}

func toModel(
	id int64,
	externalID pgtype.UUID,
	username string,
	createdAt pgtype.Timestamptz,
) model.User {
	return model.User{
		ID:         id,
		ExternalID: externalID.String(),
		Username:   username,
		CreatedAt:  createdAt.Time,
	}
}
