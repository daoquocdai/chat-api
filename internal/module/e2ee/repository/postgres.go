package repository

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *PostgresRepository { return &PostgresRepository{pool: pool} }

func (r *PostgresRepository) Upload(ctx context.Context, userID int64, request e2ee.UploadRequest) (e2ee.UploadResult, error) {
	var result e2ee.UploadResult
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		user, err := q.LockE2EEUser(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return usermodel.ErrUserNotFound
		}
		if err != nil {
			return err
		}
		identity, err := decode(request.IdentityPublicKey, 32)
		if err != nil {
			return err
		}
		spk, err := decode(request.SignedPrekey.PublicKey, 32)
		if err != nil {
			return err
		}
		signature, err := decode(request.SignedPrekey.Signature, 64)
		if err != nil {
			return err
		}
		stored, err := q.GetE2EESignedPrekeys(ctx, userID)
		if err != nil {
			return err
		}
		// Capture the floor once: an unsorted batch may contain several new IDs.
		floor, highest := user.LastPrekeyID, user.LastPrekeyID
		if len(user.IdentityPublicKey) == 0 && len(stored) == 0 && floor == 0 {
			if err := q.InsertE2EEPrekey(ctx, sqlc.InsertE2EEPrekeyParams{
				UserID: userID, KeyID: request.SignedPrekey.KeyID, Kind: "signed",
				PublicKey: spk, Signature: signature,
			}); err != nil {
				return keyConflict(err)
			}
			highest = request.SignedPrekey.KeyID
		} else if len(stored) != 1 || !bytes.Equal(user.IdentityPublicKey, identity) ||
			stored[0].KeyID != request.SignedPrekey.KeyID ||
			!bytes.Equal(stored[0].PublicKey, spk) || !bytes.Equal(stored[0].Signature, signature) {
			return model.ErrKeyConflict
		}
		for _, opk := range request.OneTimePrekeys {
			public, err := decode(opk.PublicKey, 32)
			if err != nil {
				return err
			}
			if opk.KeyID <= floor {
				old, err := q.GetE2EEPrekey(ctx, sqlc.GetE2EEPrekeyParams{UserID: userID, KeyID: opk.KeyID})
				if errors.Is(err, pgx.ErrNoRows) {
					// Consumed or skipped ID: never restore an old key. No tombstone.
					continue
				}
				if err != nil {
					return err
				}
				if old.Kind != "one_time" || !bytes.Equal(old.PublicKey, public) {
					return model.ErrKeyConflict
				}
				continue
			}
			if err := q.InsertE2EEPrekey(ctx, sqlc.InsertE2EEPrekeyParams{
				UserID: userID, KeyID: opk.KeyID, Kind: "one_time", PublicKey: public,
			}); err != nil {
				return keyConflict(err)
			}
			if opk.KeyID > highest {
				highest = opk.KeyID
			}
		}
		if err := q.SetE2EEUserKeys(ctx, sqlc.SetE2EEUserKeysParams{
			UserID: userID, IdentityPublicKey: identity, LastPrekeyID: highest,
		}); err != nil {
			return err
		}
		count, err := q.CountE2EEOneTimePrekeys(ctx, userID)
		if err != nil {
			return err
		}
		result = e2ee.UploadResult{
			UserID: user.ExternalID.String(), IdentityPublicKey: request.IdentityPublicKey,
			SignedPrekeyID: request.SignedPrekey.KeyID, LastPrekeyID: highest, OneTimePrekeyCount: count,
		}
		return nil
	})
	if err != nil {
		return e2ee.UploadResult{}, err
	}
	// BeginFunc has committed. Never expose success when commit failed.
	return result, nil
}

func (r *PostgresRepository) Claim(ctx context.Context, actorID, recipientID int64, threadExternalID string) (e2ee.Bundle, error) {
	if actorID == recipientID {
		return e2ee.Bundle{}, model.ErrForbidden
	}
	var threadUUID pgtype.UUID
	if err := threadUUID.Scan(threadExternalID); err != nil {
		return e2ee.Bundle{}, model.ErrInvalidClaim
	}
	var bundle e2ee.Bundle
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		// Keep mode and membership stable through the consume and commit.
		thread, err := q.GetE2EEClaimThread(ctx, threadUUID)
		if errors.Is(err, pgx.ErrNoRows) {
			return threadmodel.ErrThreadNotFound
		}
		if err != nil {
			return err
		}
		if thread.Kind != "direct" || thread.EncryptionMode != "e2ee" {
			return model.ErrThreadMode
		}
		participants, err := q.GetE2EEClaimParticipants(ctx, thread.ID)
		if err != nil {
			return err
		}
		actorActive, recipientActive := false, false
		for _, id := range participants {
			actorActive = actorActive || id == actorID
			recipientActive = recipientActive || id == recipientID
		}
		if len(participants) != 2 || !actorActive || !recipientActive {
			return model.ErrForbidden
		}
		user, err := q.LockE2EEUser(ctx, recipientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return usermodel.ErrUserNotFound
		}
		if err != nil {
			return err
		}
		stored, err := q.GetE2EESignedPrekeys(ctx, recipientID)
		if err != nil {
			return err
		}
		if len(user.IdentityPublicKey) == 0 || len(stored) == 0 {
			return model.ErrBundleNotFound
		}
		if len(stored) != 1 {
			return errors.New("invalid stored signed prekey state")
		}
		bundle = e2ee.Bundle{
			UserID: user.ExternalID.String(), IdentityPublicKey: base64.StdEncoding.EncodeToString(user.IdentityPublicKey),
			SignedPrekey: e2ee.SignedPrekey{
				KeyID: stored[0].KeyID, PublicKey: base64.StdEncoding.EncodeToString(stored[0].PublicKey),
				Signature: base64.StdEncoding.EncodeToString(stored[0].Signature),
			},
		}
		if err := e2ee.VerifyBundle(bundle); err != nil {
			return errors.New("invalid stored public prekey bundle")
		}
		opk, err := q.ConsumeE2EEOneTimePrekey(ctx, recipientID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			bundle.OneTimePrekey = &e2ee.PublicPrekey{KeyID: opk.KeyID, PublicKey: base64.StdEncoding.EncodeToString(opk.PublicKey)}
			if err := e2ee.VerifyBundle(bundle); err != nil {
				return errors.New("invalid stored one-time prekey")
			}
		}
		return nil
	})
	if err != nil {
		return e2ee.Bundle{}, err
	}
	return bundle, nil
}

func decode(value string, length int) ([]byte, error) {
	decoded, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || len(decoded) != length || base64.StdEncoding.EncodeToString(decoded) != value {
		return nil, model.ErrInvalidPrekeys
	}
	return decoded, nil
}

func keyConflict(err error) error {
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return model.ErrKeyConflict
	}
	return err
}
