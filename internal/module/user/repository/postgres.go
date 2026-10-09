package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool    *pgxpool.Pool
	queries *sqlc.Queries
}

func New(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, queries: sqlc.New(pool)}
}

func (r *PostgresRepository) CreateAccount(ctx context.Context, registration model.AccountRegistration) (model.User, error) {
	kdf, err := json.Marshal(registration.KDF)
	if err != nil {
		return model.User{}, err
	}
	publicBundle, err := json.Marshal(registration.PublicBundle)
	if err != nil {
		return model.User{}, err
	}
	accountVault, err := json.Marshal(registration.AccountVault)
	if err != nil {
		return model.User{}, err
	}

	var saved model.User
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		queries := sqlc.New(tx)
		user, err := queries.CreateUserAccount(ctx, sqlc.CreateUserAccountParams{
			Username: registration.Username, AuthCredentialHash: registration.AuthCredentialHash,
			Kdf: kdf, PublicBundle: publicBundle, AccountVault: accountVault,
		})
		if err != nil {
			return err
		}
		for _, prekey := range registration.PublicBundle.OneTimePrekeys {
			publicKey, err := base64.StdEncoding.Strict().DecodeString(prekey.PublicKey)
			if err != nil {
				return err
			}
			if err := queries.InsertE2EEOneTimePrekey(ctx, sqlc.InsertE2EEOneTimePrekeyParams{
				UserID: user.ID, KeyID: prekey.KeyID, PublicKey: publicKey,
			}); err != nil {
				return err
			}
		}
		saved = toModel(user.ID, user.ExternalID, user.Username, user.CreatedAt)
		return nil
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

	return saved, nil
}

func (r *PostgresRepository) GetAuthParamsByUsername(ctx context.Context, username string) (model.AuthParams, error) {
	stored, err := r.queries.GetAuthParamsByUsername(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.AuthParams{}, model.ErrUserNotFound
	}
	if err != nil {
		return model.AuthParams{}, err
	}
	params := model.AuthParams{Username: stored.Username}
	if err := json.Unmarshal(stored.Kdf, &params.KDF); err != nil {
		return model.AuthParams{}, err
	}
	return params, nil
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
		User:               toModel(user.ID, user.ExternalID, user.Username, user.CreatedAt),
		AuthCredentialHash: user.AuthCredentialHash,
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
