package e2ee

// KDFProfile is deliberately fixed. Clients reject unrecognized parameters
// before allocating memory or deriving a credential.
type KDFProfile struct {
	Version     int    `json:"version"`
	Algorithm   string `json:"algorithm"`
	Salt        string `json:"salt"`
	MemoryKiB   uint32 `json:"memory_kib"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
}

type PasswordKeys struct {
	AuthCredential string
	VaultKey       [32]byte
}

type EncryptedRecord struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type PrivateKeyPair struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

type VaultSignedPrekey struct {
	KeyID     int64          `json:"key_id"`
	KeyPair   PrivateKeyPair `json:"key_pair"`
	Signature string         `json:"signature"`
}

type VaultOneTimePrekey struct {
	KeyID   int64          `json:"key_id"`
	KeyPair PrivateKeyPair `json:"key_pair"`
}

// AccountVault is client plaintext only. All initial OPKs remain archived so a
// recipient can recover an epoch it has never previously opened on any device.
type AccountVault struct {
	Version        int                  `json:"version"`
	Identity       PrivateKeyPair       `json:"identity"`
	SignedPrekey   VaultSignedPrekey    `json:"signed_prekey"`
	OneTimePrekeys []VaultOneTimePrekey `json:"one_time_prekeys"`
}

type EpochContext struct {
	ThreadID    string `json:"thread_id"`
	EpochID     string `json:"epoch_id"`
	SenderID    string `json:"sender_id"`
	RecipientID string `json:"recipient_id"`
}

type EpochHeader struct {
	Version              int    `json:"version"`
	ThreadID             string `json:"thread_id"`
	EpochID              string `json:"epoch_id"`
	SenderID             string `json:"sender_id"`
	RecipientID          string `json:"recipient_id"`
	SenderIdentityKey    string `json:"sender_identity_key"`
	RecipientIdentityKey string `json:"recipient_identity_key"`
	EphemeralKey         string `json:"ephemeral_key"`
	SignedPrekeyID       int64  `json:"signed_prekey_id"`
	OneTimePrekeyID      *int64 `json:"one_time_prekey_id"`
	Nonce                string `json:"nonce"`
	Ciphertext           string `json:"ciphertext"`
}

type EpochMessageContext struct {
	ThreadID    string `json:"thread_id"`
	EpochID     string `json:"epoch_id"`
	MessageID   string `json:"message_id"`
	SenderID    string `json:"sender_id"`
	RecipientID string `json:"recipient_id"`
}

type MessageEnvelope struct {
	Version     int    `json:"version"`
	EpochID     string `json:"epoch_id"`
	RecipientID string `json:"recipient_id"`
	Nonce       string `json:"nonce"`
	Ciphertext  string `json:"ciphertext"`
}
