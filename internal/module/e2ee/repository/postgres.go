package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/daoquocdai/chat-api/internal/database/sqlc"
	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	"github.com/daoquocdai/chat-api/internal/module/e2ee/model"
	threadmodel "github.com/daoquocdai/chat-api/internal/module/thread/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *PostgresRepository { return &PostgresRepository{pool: pool} }

func (r *PostgresRepository) Account(ctx context.Context, userID int64) (dto.AccountResponse, error) {
	row, err := sqlc.New(r.pool).GetUserAccount(ctx, userID)
	if err != nil {
		return dto.AccountResponse{}, err
	}
	result := dto.AccountResponse{UserID: row.ExternalID.String(), Username: row.Username}
	if err = json.Unmarshal(row.Kdf, &result.KDF); err != nil {
		return result, err
	}
	if err = json.Unmarshal(row.PublicBundle, &result.PublicBundle); err != nil {
		return result, err
	}
	err = json.Unmarshal(row.AccountVault, &result.AccountVault)
	return result, err
}

func (r *PostgresRepository) Claim(ctx context.Context, actorID, recipientID int64, threadExternalID string) (e2ee.Bundle, error) {
	id, err := parseUUID(threadExternalID)
	if err != nil {
		return e2ee.Bundle{}, err
	}
	var result e2ee.Bundle
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		thread, err := q.GetE2EEClaimThread(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return threadmodel.ErrThreadNotFound
		}
		if err != nil {
			return err
		}
		if thread.Kind != "direct" {
			return model.ErrNotDirectThread
		}
		members, err := q.GetE2EEClaimParticipants(ctx, thread.ID)
		if err != nil {
			return err
		}
		actor, recipient := false, false
		for _, member := range members {
			actor = actor || member == actorID
			recipient = recipient || member == recipientID
		}
		if len(members) != 2 || !actor || !recipient || actorID == recipientID {
			return model.ErrForbidden
		}
		user, err := q.LockE2EEUser(ctx, recipientID)
		if err != nil {
			return err
		}
		var public e2ee.UploadRequest
		if err = json.Unmarshal(user.PublicBundle, &public); err != nil {
			return err
		}
		result = e2ee.Bundle{UserID: user.ExternalID.String(), IdentityPublicKey: public.IdentityPublicKey, SignedPrekey: public.SignedPrekey}
		if err = e2ee.VerifyBundle(result); err != nil {
			return model.ErrBundleNotFound
		}
		opk, err := q.ConsumeE2EEOneTimePrekey(ctx, recipientID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil {
			result.OneTimePrekey = &e2ee.PublicPrekey{KeyID: opk.KeyID, PublicKey: base64.StdEncoding.EncodeToString(opk.PublicKey)}
		}
		return nil
	})
	return result, err
}

func (r *PostgresRepository) Epochs(ctx context.Context, userID int64, threadExternalID string) (dto.EpochPageResponse, error) {
	id, err := parseUUID(threadExternalID)
	if err != nil {
		return dto.EpochPageResponse{}, err
	}
	q := sqlc.New(r.pool)
	thread, err := q.GetEpochThreadForParticipant(ctx, sqlc.GetEpochThreadForParticipantParams{ThreadExternalID: id, UserID: userID})
	if err != nil {
		return dto.EpochPageResponse{}, threadError(ctx, q, id, err)
	}
	if thread.Kind != "direct" {
		return dto.EpochPageResponse{}, model.ErrNotDirectThread
	}
	rows, err := q.ListEpochsForUser(ctx, sqlc.ListEpochsForUserParams{ThreadID: thread.ID, UserID: userID})
	if err != nil {
		return dto.EpochPageResponse{}, err
	}
	result := dto.EpochPageResponse{Epochs: make([]dto.EpochResponse, 0, len(rows))}
	if thread.CurrentEpochID.Valid {
		current := thread.CurrentEpochID.String()
		result.CurrentEpochID = &current
	}
	for _, row := range rows {
		epoch, err := epochResponse(sqlc.GetEpochForUserRow(row))
		if err != nil {
			return result, err
		}
		result.Epochs = append(result.Epochs, epoch)
	}
	return result, nil
}

func (r *PostgresRepository) CreateEpoch(ctx context.Context, actorID int64, actorExternalID, threadExternalID string, request dto.CreateEpochRequest) (dto.EpochResponse, bool, error) {
	threadID, err := parseUUID(threadExternalID)
	if err != nil {
		return dto.EpochResponse{}, false, err
	}
	epochID, err := parseUUID(request.EpochID)
	if err != nil {
		return dto.EpochResponse{}, false, err
	}
	header, err := e2ee.ParseEpochHeader(request.Bootstrap)
	if err != nil {
		return dto.EpochResponse{}, false, model.ErrInvalidEpoch
	}
	if header.ThreadID != threadExternalID || header.EpochID != request.EpochID || header.SenderID != actorExternalID {
		return dto.EpochResponse{}, false, model.ErrInvalidEpoch
	}
	if err = e2ee.ValidateEncryptedRecord(request.KeyBackup, 1024); err != nil {
		return dto.EpochResponse{}, false, model.ErrInvalidEpoch
	}
	var previous pgtype.UUID
	if request.PreviousEpochID != nil {
		previous, err = parseUUID(*request.PreviousEpochID)
		if err != nil {
			return dto.EpochResponse{}, false, err
		}
	}
	var result dto.EpochResponse
	created := false
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		thread, err := q.LockEpochThreadForParticipant(ctx, sqlc.LockEpochThreadForParticipantParams{ThreadExternalID: threadID, UserID: actorID})
		if err != nil {
			return threadError(ctx, q, threadID, err)
		}
		if thread.Kind != "direct" {
			return model.ErrNotDirectThread
		}
		lookup := sqlc.GetEpochForUserParams{EpochID: epochID, ThreadID: thread.ID, UserID: actorID}
		existing, err := q.GetEpochForUser(ctx, lookup)
		if err == nil {
			result, err = epochResponse(existing)
			if err != nil {
				return err
			}
			if result.SenderID != actorExternalID || result.Bootstrap != request.Bootstrap || result.KeyBackup == nil || *result.KeyBackup != request.KeyBackup {
				return model.ErrInvalidEpoch
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if thread.CurrentEpochID != previous {
			if !thread.CurrentEpochID.Valid {
				return model.ErrEpochConflict
			}
			lookup.EpochID = thread.CurrentEpochID
			winner, err := q.GetEpochForUser(ctx, lookup)
			if err != nil {
				return err
			}
			canonical, err := epochResponse(winner)
			if err != nil {
				return err
			}
			return &dto.EpochConflictError{Epoch: canonical}
		}
		profiles, err := q.GetEpochParticipantProfiles(ctx, thread.ID)
		if err != nil {
			return err
		}
		if len(profiles) != 2 {
			return model.ErrForbidden
		}
		var recipientID int64
		var senderValid, recipientValid bool
		for _, profile := range profiles {
			var public e2ee.UploadRequest
			if err = json.Unmarshal(profile.PublicBundle, &public); err != nil {
				return err
			}
			if profile.ID == actorID {
				senderValid = header.SenderIdentityKey == public.IdentityPublicKey
				continue
			}
			if profile.ExternalID.String() != header.RecipientID {
				continue
			}
			recipientID = profile.ID
			recipientValid = header.RecipientIdentityKey == public.IdentityPublicKey && header.SignedPrekeyID == public.SignedPrekey.KeyID
			if header.OneTimePrekeyID != nil {
				found := false
				for _, opk := range public.OneTimePrekeys {
					found = found || opk.KeyID == *header.OneTimePrekeyID
				}
				recipientValid = recipientValid && found
			}
		}
		if !senderValid || !recipientValid || recipientID == 0 {
			return model.ErrInvalidEpoch
		}
		if err = q.CreateEpoch(ctx, sqlc.CreateEpochParams{ID: epochID, ThreadID: thread.ID, SenderID: actorID, RecipientID: recipientID, Bootstrap: request.Bootstrap}); err != nil {
			return err
		}
		backup, err := json.Marshal(request.KeyBackup)
		if err != nil {
			return err
		}
		if err = q.StoreEpochBackup(ctx, sqlc.StoreEpochBackupParams{EpochID: epochID, UserID: actorID, KeyBackup: backup}); err != nil {
			return err
		}
		if err = q.SetCurrentEpoch(ctx, sqlc.SetCurrentEpochParams{ThreadID: thread.ID, EpochID: epochID}); err != nil {
			return err
		}
		stored, err := q.GetEpochForUser(ctx, lookup)
		if err != nil {
			return err
		}
		result, err = epochResponse(stored)
		created = err == nil
		return err
	})
	return result, created, err
}

func (r *PostgresRepository) Backup(ctx context.Context, userID int64, threadExternalID, epochExternalID string, backup e2ee.EncryptedRecord) (e2ee.EncryptedRecord, error) {
	threadID, err := parseUUID(threadExternalID)
	if err != nil {
		return e2ee.EncryptedRecord{}, err
	}
	epochID, err := parseUUID(epochExternalID)
	if err != nil {
		return e2ee.EncryptedRecord{}, err
	}
	var result e2ee.EncryptedRecord
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		thread, err := q.GetEpochThreadForParticipant(ctx, sqlc.GetEpochThreadForParticipantParams{ThreadExternalID: threadID, UserID: userID})
		if err != nil {
			return threadError(ctx, q, threadID, err)
		}
		if thread.Kind != "direct" {
			return model.ErrNotDirectThread
		}
		lookup := sqlc.GetEpochForUserParams{EpochID: epochID, ThreadID: thread.ID, UserID: userID}
		if _, err = q.GetEpochForUser(ctx, lookup); errors.Is(err, pgx.ErrNoRows) {
			return model.ErrEpochNotFound
		}
		if err != nil {
			return err
		}
		data, err := json.Marshal(backup)
		if err != nil {
			return err
		}
		if err = q.StoreEpochBackup(ctx, sqlc.StoreEpochBackupParams{EpochID: epochID, UserID: userID, KeyBackup: data}); err != nil {
			return err
		}
		stored, err := q.GetEpochForUser(ctx, lookup)
		if err != nil {
			return err
		}
		return json.Unmarshal(stored.KeyBackup, &result)
	})
	return result, err
}

func epochResponse(row sqlc.GetEpochForUserRow) (dto.EpochResponse, error) {
	result := dto.EpochResponse{EpochID: row.ID.String(), ThreadID: row.ThreadExternalID.String(), SenderID: row.SenderExternalID.String(), RecipientID: row.RecipientExternalID.String(), Bootstrap: row.Bootstrap}
	if len(row.KeyBackup) != 0 {
		result.KeyBackup = &e2ee.EncryptedRecord{}
		if err := json.Unmarshal(row.KeyBackup, result.KeyBackup); err != nil {
			return result, err
		}
	}
	return result, nil
}

func threadError(ctx context.Context, q *sqlc.Queries, id pgtype.UUID, err error) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	exists, err := q.ThreadExistsByExternalID(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return threadmodel.ErrThreadNotFound
	}
	return threadmodel.ErrNotParticipant
}

func parseUUID(value string) (pgtype.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return pgtype.UUID{}, model.ErrInvalidEpoch
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}
