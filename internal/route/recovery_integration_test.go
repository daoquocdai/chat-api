package route_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daoquocdai/chat-api/config"
	"github.com/daoquocdai/chat-api/internal/e2ee"
	"github.com/daoquocdai/chat-api/internal/middleware"
	e2eedto "github.com/daoquocdai/chat-api/internal/module/e2ee/dto"
	e2eehandler "github.com/daoquocdai/chat-api/internal/module/e2ee/handler"
	e2eerepository "github.com/daoquocdai/chat-api/internal/module/e2ee/repository"
	e2eeservice "github.com/daoquocdai/chat-api/internal/module/e2ee/service"
	messagedto "github.com/daoquocdai/chat-api/internal/module/message/dto"
	messagehandler "github.com/daoquocdai/chat-api/internal/module/message/handler"
	messagepublisher "github.com/daoquocdai/chat-api/internal/module/message/publisher"
	messagerepository "github.com/daoquocdai/chat-api/internal/module/message/repository"
	messageservice "github.com/daoquocdai/chat-api/internal/module/message/service"
	threaddto "github.com/daoquocdai/chat-api/internal/module/thread/dto"
	threadhandler "github.com/daoquocdai/chat-api/internal/module/thread/handler"
	"github.com/daoquocdai/chat-api/internal/module/thread/membership"
	threadrepository "github.com/daoquocdai/chat-api/internal/module/thread/repository"
	threadservice "github.com/daoquocdai/chat-api/internal/module/thread/service"
	userdto "github.com/daoquocdai/chat-api/internal/module/user/dto"
	userhandler "github.com/daoquocdai/chat-api/internal/module/user/handler"
	usermodel "github.com/daoquocdai/chat-api/internal/module/user/model"
	userrepository "github.com/daoquocdai/chat-api/internal/module/user/repository"
	userservice "github.com/daoquocdai/chat-api/internal/module/user/service"
	"github.com/daoquocdai/chat-api/internal/route"
	"github.com/daoquocdai/chat-api/internal/token"
	"github.com/daoquocdai/chat-api/internal/wsticket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

// Run explicitly against the disposable local project database after migrations.
// Accounts use unique names; this test never truncates unrelated project data.
func TestRecoverableE2EEIntegration(t *testing.T) {
	if os.Getenv("CHAT_API_INTEGRATION") != "1" {
		t.Skip("set CHAT_API_INTEGRATION=1 to test local PostgreSQL and Redis")
	}
	cfg, err := config.Load("../../config/config.yml")
	if err != nil {
		t.Fatal("integration configuration unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatal("integration database configuration invalid")
	}
	defer pool.Close()
	connection := pool.Config().ConnConfig
	if connection.Database != "chat_api" || (connection.Host != "localhost" && connection.Host != "127.0.0.1" && connection.Host != "::1") {
		t.Fatal("integration requires the local chat_api project database")
	}
	if pool.Ping(ctx) != nil {
		t.Fatal("integration PostgreSQL unavailable")
	}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address, Password: cfg.Redis.Password, DB: cfg.Redis.Database})
	defer rdb.Close()
	if rdb.Ping(ctx).Err() != nil {
		t.Fatal("integration Redis unavailable")
	}
	stream := cfg.Redis.Stream + ":integration:" + uuid.NewString()
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		iterator := rdb.Scan(cleanup, 0, stream+"*", 100).Iterator()
		for iterator.Next(cleanup) {
			_ = rdb.Del(cleanup, iterator.Val()).Err()
		}
	}()
	jwt, err := token.NewJWT(cfg.Auth.JWTSecret, cfg.Auth.JWTTTL)
	if err != nil {
		t.Fatal("integration JWT configuration invalid")
	}
	userRepo := userrepository.New(pool)
	users := userservice.New(userRepo, jwt)
	messages := messageservice.New(messagerepository.New(pool), users, messagepublisher.NewRedis(rdb, stream), membership.NewRedis(rdb, stream), time.Second)
	threads := threadservice.New(threadrepository.New(pool), users, messages)
	cryptoService := e2eeservice.New(e2eerepository.New(pool), users)
	tickets := wsticket.NewService(wsticket.NewRedisRepository(rdb), cfg.WSTicketTTL)
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(route.New(userhandler.New(users), threadhandler.New(threads), messagehandler.New(messages), wsticket.NewHandler(tickets, cfg.WSPublicURL, time.Second), e2eehandler.New(cryptoService), middleware.RequireAuthentication(jwt)))
	defer server.Close()
	api := recoveryAPI{t: t, base: server.URL, client: &http.Client{Timeout: 10 * time.Second}}
	alice := api.register("it_alice_" + uuid.NewString()[:8])
	bob := api.register("it_bob_" + uuid.NewString()[:8])
	charlie := api.register("it_charlie_" + uuid.NewString()[:8])
	var count int
	if pool.QueryRow(ctx, "SELECT COUNT(*) FROM prekeys JOIN users ON users.id = prekeys.user_id WHERE users.external_id = $1::uuid", alice.id).Scan(&count) != nil || count != 20 {
		t.Fatal("registration did not commit all 20 public OPKs")
	}
	var hash string
	if pool.QueryRow(ctx, "SELECT auth_credential_hash FROM users WHERE external_id = $1::uuid", alice.id).Scan(&hash) != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(alice.keys.AuthCredential)) != nil {
		t.Fatal("registration credential hash does not authenticate")
	}
	// A failure after inserting the user must roll back the vault and all OPKs.
	badBundle := alice.public
	badBundle.OneTimePrekeys = append(append([]e2ee.PublicPrekey{}, badBundle.OneTimePrekeys...), badBundle.OneTimePrekeys[0])
	failedName := "it_rollback_" + uuid.NewString()[:8]
	_, err = userRepo.CreateAccount(ctx, usermodel.AccountRegistration{Username: failedName, AuthCredentialHash: hash, KDF: alice.kdf, PublicBundle: badBundle, AccountVault: alice.encrypted})
	if err == nil || pool.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE username = $1", failedName).Scan(&count) != nil || count != 0 {
		t.Fatal("failed registration left a partial account")
	}
	api.require(http.MethodPost, "/auth/login", "", map[string]string{"username": alice.username, "auth_credential": bob.keys.AuthCredential}, http.StatusUnauthorized, nil)
	api.require(http.MethodPost, "/auth/login", "", map[string]string{"username": alice.username, "password": alice.password}, http.StatusBadRequest, nil)
	api.require(http.MethodPost, "/auth/register", "", map[string]string{"username": failedName, "password": alice.password}, http.StatusBadRequest, nil)
	var direct threaddto.ThreadResponse
	api.require(http.MethodPost, "/threads/direct", alice.token, threaddto.CreateDirectRequest{PeerID: bob.id}, http.StatusCreated, &direct)
	var reused threaddto.ThreadResponse
	api.require(http.MethodPost, "/threads/direct", bob.token, threaddto.CreateDirectRequest{PeerID: alice.id}, http.StatusOK, &reused)
	if reused.ID != direct.ID {
		t.Fatal("the same peer pair created a second direct thread")
	}

	// Two devices race initial proposals. Each stores a sender backup atomically;
	// the losing device must recover the committed epoch, never use its own SK.
	proposals := []recoveryProposal{api.proposal(alice, bob.id, direct.ID, nil), api.proposal(alice, bob.id, direct.ID, nil)}
	type raceReply struct {
		index, status int
		body          []byte
		err           error
	}
	answers := make(chan raceReply, 2)
	start := make(chan struct{})
	for index := range proposals {
		go func(index int) {
			<-start
			status, body, err := api.request(http.MethodPost, "/threads/"+direct.ID+"/epochs", alice.token, proposals[index].request)
			answers <- raceReply{index: index, status: status, body: body, err: err}
		}(index)
	}
	close(start)
	results := []raceReply{<-answers, <-answers}
	winnerIndex := -1
	var winner, conflictWinner e2eedto.EpochResponse
	for _, result := range results {
		if result.err != nil {
			t.Fatal("concurrent epoch HTTP request failed")
		}
		switch result.status {
		case http.StatusCreated:
			if winnerIndex != -1 || json.Unmarshal(result.body, &winner) != nil {
				t.Fatal("multiple initial epochs were committed")
			}
			winnerIndex = result.index
		case http.StatusConflict:
			var conflict struct {
				Epoch e2eedto.EpochResponse `json:"epoch"`
			}
			if json.Unmarshal(result.body, &conflict) != nil {
				t.Fatal("epoch conflict response is invalid")
			}
			conflictWinner = conflict.Epoch
		default:
			t.Fatalf("concurrent epoch status = %d", result.status)
		}
	}
	if winnerIndex == -1 || winner.EpochID != conflictWinner.EpochID || winner.KeyBackup == nil || !reflect.DeepEqual(winner, conflictWinner) {
		t.Fatal("the race did not return one canonical epoch with its sender backup")
	}
	selected := proposals[winnerIndex]
	if pool.QueryRow(ctx, "SELECT COUNT(*) FROM e2ee_epochs JOIN threads ON threads.id = e2ee_epochs.thread_id WHERE threads.external_id = $1::uuid", direct.ID).Scan(&count) != nil || count != 1 {
		t.Fatal("the initial epoch race persisted a losing proposal")
	}
	if pool.QueryRow(ctx, "SELECT COUNT(*) FROM e2ee_epoch_backups WHERE epoch_id = $1::uuid", winner.EpochID).Scan(&count) != nil || count != 1 {
		t.Fatal("initial epoch and sender backup were not committed together")
	}
	api.require(http.MethodPost, "/threads/"+direct.ID+"/epochs", alice.token, selected.request, http.StatusOK, nil)

	// Discard the recipient's original local keys before its first decryption.
	originalBobPublic := bob.public
	bob.vault, bob.keys = e2ee.AccountVault{}, e2ee.PasswordKeys{}
	bob = api.restore(bob)
	if !reflect.DeepEqual(bob.public, originalBobPublic) {
		t.Fatal("claim changed the immutable registration public bundle")
	}
	var bobEpochs e2eedto.EpochPageResponse
	api.require(http.MethodGet, "/threads/"+direct.ID+"/epochs", bob.token, nil, http.StatusOK, &bobEpochs)
	if len(bobEpochs.Epochs) != 1 || bobEpochs.Epochs[0].KeyBackup != nil {
		t.Fatal("recipient saw another account's private key backup")
	}
	bobSK := api.recoverBootstrap(bob, selected.header)
	if bobSK != selected.key {
		t.Fatal("fresh recipient device could not recover an unopened epoch")
	}
	backup := cryptoValue[e2ee.EncryptedRecord](t)(e2ee.EncryptEpochBackup(bob.keys.VaultKey, bob.id, selected.header, bobSK))
	var committed e2eedto.BackupRequest
	api.require(http.MethodPut, "/threads/"+direct.ID+"/epochs/"+winner.EpochID+"/key", bob.token, e2eedto.BackupRequest{KeyBackup: backup}, http.StatusOK, &committed)
	secondBackup := cryptoValue[e2ee.EncryptedRecord](t)(e2ee.EncryptEpochBackup(bob.keys.VaultKey, bob.id, selected.header, bobSK))
	api.require(http.MethodPut, "/threads/"+direct.ID+"/epochs/"+winner.EpochID+"/key", bob.token, e2eedto.BackupRequest{KeyBackup: secondBackup}, http.StatusOK, &committed)
	if committed.KeyBackup != backup {
		t.Fatal("a later device overwrote the first committed key backup")
	}

	firstRequest, first := api.send(alice, bob.id, direct.ID, selected, "sent from Alice")
	_, reply := api.send(bob, alice.id, direct.ID, selected, "reply from Bob")
	var retried messagedto.MessageResponse
	api.require(http.MethodPost, "/threads/"+direct.ID+"/messages", alice.token, firstRequest, http.StatusOK, &retried)
	if !reflect.DeepEqual(first, retried) || reply.Seq != first.Seq+1 {
		t.Fatal("message retry changed UUID, payload or sequence")
	}
	changed := firstRequest
	changed.Content += " "
	api.require(http.MethodPost, "/threads/"+direct.ID+"/messages", alice.token, changed, http.StatusConflict, nil)

	rotated := api.proposal(alice, bob.id, direct.ID, &winner.EpochID)
	api.require(http.MethodPost, "/threads/"+direct.ID+"/epochs", alice.token, rotated.request, http.StatusCreated, nil)
	_, latest := api.send(alice, bob.id, direct.ID, rotated, "sent after rotation")
	for _, account := range []recoveryAccount{alice, bob} {
		account.vault, account.keys = e2ee.AccountVault{}, e2ee.PasswordKeys{}
		account = api.restore(account)
		var epochs e2eedto.EpochPageResponse
		api.require(http.MethodGet, "/threads/"+direct.ID+"/epochs", account.token, nil, http.StatusOK, &epochs)
		if len(epochs.Epochs) != 2 || epochs.CurrentEpochID == nil || *epochs.CurrentEpochID != rotated.request.EpochID {
			t.Fatal("fresh device did not receive both old and current epochs")
		}
		keys := map[string][32]byte{}
		for _, epoch := range epochs.Epochs {
			header := cryptoValue[e2ee.EpochHeader](t)(e2ee.ParseEpochHeader(epoch.Bootstrap))
			if epoch.KeyBackup != nil {
				keys[epoch.EpochID] = cryptoValue[[32]byte](t)(e2ee.OpenEpochBackup(account.keys.VaultKey, account.id, header, *epoch.KeyBackup))
			} else {
				keys[epoch.EpochID] = api.recoverBootstrap(account, header)
			}
		}
		var history messagedto.MessagePageResponse
		api.require(http.MethodGet, "/threads/"+direct.ID+"/messages", account.token, nil, http.StatusOK, &history)
		if len(history.Messages) != 3 {
			t.Fatal("history lost messages or included a retry duplicate")
		}
		expected := map[string]string{first.ID: "sent from Alice", reply.ID: "reply from Bob", latest.ID: "sent after rotation"}
		for _, message := range history.Messages {
			envelope := cryptoValue[e2ee.MessageEnvelope](t)(e2ee.ParseMessageEnvelope(message.Content))
			plaintext := cryptoValue[[]byte](t)(e2ee.OpenMessage(e2ee.EpochMessageContext{ThreadID: direct.ID, EpochID: envelope.EpochID, MessageID: message.ID, SenderID: message.SenderID, RecipientID: envelope.RecipientID}, keys[envelope.EpochID], envelope))
			if string(plaintext) != expected[message.ID] {
				t.Fatal("a fresh device could not decrypt sent and received history")
			}
			clear(plaintext)
		}
	}

	var otherThread threaddto.ThreadResponse
	api.require(http.MethodPost, "/threads/direct", alice.token, threaddto.CreateDirectRequest{PeerID: charlie.id}, http.StatusCreated, &otherThread)
	for _, invalid := range []struct{ thread, recipient, epoch string }{
		{otherThread.ID, charlie.id, winner.EpochID},
		{direct.ID, charlie.id, winner.EpochID},
		{direct.ID, bob.id, uuid.NewString()},
	} {
		request := encryptedMessage(t, alice.id, invalid.recipient, invalid.thread, invalid.epoch, selected.key, "invalid context")
		api.require(http.MethodPost, "/threads/"+invalid.thread+"/messages", alice.token, request, http.StatusBadRequest, nil)
	}
	api.require(http.MethodPost, "/threads/"+direct.ID+"/messages", charlie.token, encryptedMessage(t, charlie.id, bob.id, direct.ID, winner.EpochID, selected.key, "not a member"), http.StatusForbidden, nil)
	api.require(http.MethodPost, "/threads/"+direct.ID+"/messages", alice.token, messagedto.SendMessageRequest{MessageID: uuid.NewString(), ContentFormat: "plaintext", Content: "plain direct"}, http.StatusConflict, nil)
	var group threaddto.ThreadResponse
	api.require(http.MethodPost, "/threads/group", alice.token, threaddto.CreateGroupRequest{Name: "integration group", MemberIDs: []string{bob.id}}, http.StatusCreated, &group)
	var groupMessage messagedto.MessageResponse
	api.require(http.MethodPost, "/threads/"+group.ID+"/messages", bob.token, messagedto.SendMessageRequest{MessageID: uuid.NewString(), ContentFormat: "plaintext", Content: "group still plaintext"}, http.StatusCreated, &groupMessage)
	if groupMessage.Content != "group still plaintext" || groupMessage.ContentFormat != "plaintext" {
		t.Fatal("group plaintext behavior changed")
	}
	api.require(http.MethodPost, "/threads/"+group.ID+"/messages", alice.token, encryptedMessage(t, alice.id, bob.id, group.ID, winner.EpochID, selected.key, "group encrypted"), http.StatusConflict, nil)
	api.require(http.MethodPost, "/e2ee/bundles/"+bob.id+"/claim", alice.token, e2ee.ClaimRequest{ThreadID: group.ID}, http.StatusConflict, nil)

	entries, err := rdb.XRange(ctx, stream, "-", "+").Result()
	if err != nil {
		t.Fatal("integration Redis stream read failed")
	}
	seen := map[string]int{}
	for _, entry := range entries {
		messageID, _ := entry.Values["message_id"].(string)
		if messageID != first.ID && messageID != reply.ID && messageID != groupMessage.ID {
			continue
		}
		var recipients []string
		value, _ := entry.Values["recipient_ids"].(string)
		if json.Unmarshal([]byte(value), &recipients) != nil || len(recipients) != 2 || !containsAccount(recipients, alice.id) || !containsAccount(recipients, bob.id) {
			t.Fatal("realtime omitted sender devices or the peer")
		}
		seen[messageID]++
	}
	if seen[first.ID] != 2 || seen[reply.ID] != 1 || seen[groupMessage.ID] != 1 {
		t.Fatal("realtime publish/retry did not preserve committed identities")
	}
}

type recoveryAPI struct {
	t      *testing.T
	base   string
	client *http.Client
}

type recoveryAccount struct {
	username, password, id, token string
	kdf                           e2ee.KDFProfile
	keys                          e2ee.PasswordKeys
	vault                         e2ee.AccountVault
	public                        e2ee.UploadRequest
	encrypted                     e2ee.EncryptedRecord
}

type recoveryProposal struct {
	request e2eedto.CreateEpochRequest
	header  e2ee.EpochHeader
	key     [32]byte
}

func (api recoveryAPI) request(method, path, token string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, api.base+path, reader)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := api.client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	return response.StatusCode, data, err
}

func (api recoveryAPI) require(method, path, token string, body any, status int, result any) {
	api.t.Helper()
	got, data, err := api.request(method, path, token, body)
	if err != nil {
		api.t.Fatal("integration HTTP request failed")
	}
	if got != status {
		api.t.Fatalf("integration %s status = %d, want %d", method, got, status)
	}
	if result != nil && json.Unmarshal(data, result) != nil {
		api.t.Fatal("integration response JSON invalid")
	}
}

func (api recoveryAPI) register(username string) recoveryAccount {
	api.t.Helper()
	account := recoveryAccount{username: username, password: "integration correct password"}
	account.kdf = cryptoValue[e2ee.KDFProfile](api.t)(e2ee.NewKDFProfile())
	account.keys = cryptoValue[e2ee.PasswordKeys](api.t)(e2ee.DeriveCredentials(username, account.password, account.kdf))
	var err error
	account.vault, account.public, err = e2ee.GenerateAccountVault()
	if err != nil {
		api.t.Fatal("integration account key generation failed")
	}
	account.encrypted = cryptoValue[e2ee.EncryptedRecord](api.t)(e2ee.EncryptAccountVault(account.keys.VaultKey, username, account.kdf, account.public, account.vault))
	var response userdto.UserResponse
	api.require(http.MethodPost, "/auth/register", "", userdto.RegisterRequest{Username: username, AuthCredential: account.keys.AuthCredential, KDF: account.kdf, PublicBundle: account.public, AccountVault: account.encrypted}, http.StatusCreated, &response)
	account.id = response.ID
	var login userdto.TokenResponse
	api.require(http.MethodPost, "/auth/login", "", userdto.LoginRequest{Username: username, AuthCredential: account.keys.AuthCredential}, http.StatusOK, &login)
	if login.UserID != account.id || login.TokenType != "Bearer" {
		api.t.Fatal("integration login identity changed")
	}
	account.token = login.AccessToken
	return account
}

func (api recoveryAPI) restore(account recoveryAccount) recoveryAccount {
	api.t.Helper()
	var params usermodel.AuthParams
	api.require(http.MethodGet, "/auth/params?username="+account.username, "", nil, http.StatusOK, &params)
	account.keys = cryptoValue[e2ee.PasswordKeys](api.t)(e2ee.DeriveCredentials(params.Username, account.password, params.KDF))
	var login userdto.TokenResponse
	api.require(http.MethodPost, "/auth/login", "", userdto.LoginRequest{Username: params.Username, AuthCredential: account.keys.AuthCredential}, http.StatusOK, &login)
	account.token = login.AccessToken
	var response e2eedto.AccountResponse
	api.require(http.MethodGet, "/e2ee/account", account.token, nil, http.StatusOK, &response)
	if response.UserID != account.id || response.KDF != params.KDF {
		api.t.Fatal("fresh account recovery identity or KDF changed")
	}
	account.kdf, account.public, account.encrypted = response.KDF, response.PublicBundle, response.AccountVault
	account.vault = cryptoValue[e2ee.AccountVault](api.t)(e2ee.OpenAccountVault(account.keys.VaultKey, response.Username, response.KDF, response.PublicBundle, response.AccountVault))
	return account
}

func (api recoveryAPI) proposal(sender recoveryAccount, recipientID, threadID string, previous *string) recoveryProposal {
	api.t.Helper()
	var bundle e2ee.Bundle
	api.require(http.MethodPost, "/e2ee/bundles/"+recipientID+"/claim", sender.token, e2ee.ClaimRequest{ThreadID: threadID}, http.StatusOK, &bundle)
	identity := cryptoValue[e2ee.KeyPair](api.t)(e2ee.DecodePrivateKeyPair(sender.vault.Identity))
	header, key, err := e2ee.CreateEpoch(e2ee.EpochContext{ThreadID: threadID, EpochID: uuid.NewString(), SenderID: sender.id, RecipientID: recipientID}, identity, bundle)
	if err != nil {
		api.t.Fatal("integration X3DH epoch generation failed")
	}
	backup := cryptoValue[e2ee.EncryptedRecord](api.t)(e2ee.EncryptEpochBackup(sender.keys.VaultKey, sender.id, header, key))
	return recoveryProposal{request: e2eedto.CreateEpochRequest{EpochID: header.EpochID, PreviousEpochID: previous, Bootstrap: cryptoValue[string](api.t)(e2ee.EncodeEpochHeader(header)), KeyBackup: backup}, header: header, key: key}
}

func (api recoveryAPI) recoverBootstrap(account recoveryAccount, header e2ee.EpochHeader) [32]byte {
	api.t.Helper()
	identity := cryptoValue[e2ee.KeyPair](api.t)(e2ee.DecodePrivateKeyPair(account.vault.Identity))
	signed := cryptoValue[e2ee.KeyPair](api.t)(e2ee.DecodePrivateKeyPair(account.vault.SignedPrekey.KeyPair))
	var oneTime *e2ee.KeyPair
	if header.OneTimePrekeyID != nil {
		for _, archived := range account.vault.OneTimePrekeys {
			if archived.KeyID == *header.OneTimePrekeyID {
				pair := cryptoValue[e2ee.KeyPair](api.t)(e2ee.DecodePrivateKeyPair(archived.KeyPair))
				oneTime = &pair
			}
		}
		if oneTime == nil {
			api.t.Fatal("fresh vault lost an archived private OPK")
		}
	}
	return cryptoValue[[32]byte](api.t)(e2ee.OpenEpoch(e2ee.EpochContext{ThreadID: header.ThreadID, EpochID: header.EpochID, SenderID: header.SenderID, RecipientID: header.RecipientID}, identity, signed, oneTime, header))
}

func (api recoveryAPI) send(sender recoveryAccount, recipientID, threadID string, proposal recoveryProposal, plaintext string) (messagedto.SendMessageRequest, messagedto.MessageResponse) {
	api.t.Helper()
	request := encryptedMessage(api.t, sender.id, recipientID, threadID, proposal.request.EpochID, proposal.key, plaintext)
	var message messagedto.MessageResponse
	api.require(http.MethodPost, "/threads/"+threadID+"/messages", sender.token, request, http.StatusCreated, &message)
	return request, message
}

func encryptedMessage(t *testing.T, senderID, recipientID, threadID, epochID string, key [32]byte, plaintext string) messagedto.SendMessageRequest {
	t.Helper()
	messageID := uuid.NewString()
	envelope := cryptoValue[e2ee.MessageEnvelope](t)(e2ee.SealMessage(e2ee.EpochMessageContext{ThreadID: threadID, EpochID: epochID, MessageID: messageID, SenderID: senderID, RecipientID: recipientID}, key, []byte(plaintext)))
	return messagedto.SendMessageRequest{MessageID: messageID, ContentFormat: "e2ee_v2", Content: cryptoValue[string](t)(e2ee.EncodeMessageEnvelope(envelope))}
}

func cryptoValue[T any](t *testing.T) func(T, error) T {
	t.Helper()
	return func(value T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal("integration cryptographic operation failed")
		}
		return value
	}
}

func containsAccount(ids []string, wanted string) bool {
	return strings.Contains("|"+strings.Join(ids, "|")+"|", "|"+wanted+"|")
}
