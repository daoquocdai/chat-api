// Package e2ee implements account recovery and X3DH key epochs for Mini-Hermes.
// Messages use a stateless KDF; there is no Double Ratchet. The legacy v1
// per-message functions remain for profile/vector regression tests only.
package e2ee

type KeyPair struct {
	Private [32]byte
	Public  [32]byte
}

type MessageContext struct {
	ThreadID, MessageID, SenderID, RecipientID string
}

type PublicPrekey struct {
	KeyID     int64  `json:"key_id"`
	PublicKey string `json:"public_key"`
}

type SignedPrekey struct {
	KeyID     int64  `json:"key_id"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

type UploadRequest struct {
	IdentityPublicKey string         `json:"identity_public_key"`
	SignedPrekey      SignedPrekey   `json:"signed_prekey"`
	OneTimePrekeys    []PublicPrekey `json:"one_time_prekeys"`
}

type UploadResult struct {
	UserID             string `json:"user_id"`
	IdentityPublicKey  string `json:"identity_public_key"`
	SignedPrekeyID     int64  `json:"signed_prekey_id"`
	LastPrekeyID       int64  `json:"last_prekey_id"`
	OneTimePrekeyCount int64  `json:"one_time_prekey_count"`
}

type ClaimRequest struct {
	ThreadID string `json:"thread_id"`
}

type Bundle struct {
	UserID            string        `json:"user_id"`
	IdentityPublicKey string        `json:"identity_public_key"`
	SignedPrekey      SignedPrekey  `json:"signed_prekey"`
	OneTimePrekey     *PublicPrekey `json:"one_time_prekey"`
}

type Envelope struct {
	Version           int    `json:"version"`
	RecipientID       string `json:"recipient_id"`
	SenderIdentityKey string `json:"sender_identity_key"`
	EphemeralKey      string `json:"ephemeral_key"`
	SignedPrekeyID    int64  `json:"signed_prekey_id"`
	OneTimePrekeyID   *int64 `json:"one_time_prekey_id"`
	Nonce             string `json:"nonce"`
	Ciphertext        string `json:"ciphertext"`
}
