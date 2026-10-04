# Mini-Hermes — Hợp đồng E2EE tuần 5: web Go/WASM

> Cập nhật 04/10/2026: Prompt 1–5 hoàn tất trong phạm vi demo tuần 5. Web Go/WASM đã kiểm chứng A→B và B→A, 4DH/3DH, retry/reload/catch-up và failure cases trên hai Edge profiles độc lập với PostgreSQL/Redis/gateway thật. ADR #4 và hướng dẫn demo đã có. Evidence mới nhất cùng các mục chưa kiểm chứng nằm cuối tài liệu; không suy quota/crash/browser khác PASS, không migration/reset.

Nguồn: [Mini-Hermes-tuan5-Codex-prompts.md](../Mini-Hermes-tuan5-Codex-prompts.md), bản web Go/WASM đã chốt ở bước tài liệu. Repo chưa có docs/week5-codex-prompts.md. **Toàn bộ phần II dưới đây giống nguyên văn phần II nguồn hiện hành**; profile web này thay thế thiết kế demo client trước đó. API/crypto/bridge/state/web/phase giữ nguyên; tiến độ implementation được ghi riêng cuối tài liệu.

## II. Hợp đồng triển khai cố định

### 1. Phạm vi và những quyết định giữ nguyên

1. E2EE chỉ cho chat direct trên giao diện web hiện có, một client E2EE active cho mỗi tài khoản; Web Locks chọn một tab sở hữu state trong cùng origin/profile.
2. Chat nhóm và direct plaintext tiếp tục hoạt động như hiện tại. Không đổi thread plaintext đã có sang E2EE.
3. Mỗi message dùng lượt X3DH mới, ephemeral key mới, nonce mới và UUID mới. Retry của message đó dùng nguyên dữ liệu cũ; chiều B→A claim bundle A và tạo lượt mới.
4. Không có session dùng chung nhiều message, Double Ratchet, key backup/export/import, nhiều thiết bị, đồng bộ khóa hay key rotation.
5. Identity key và signed prekey giữ nguyên trong vòng đời demo. Thêm OPK mới được phép; không tái sử dụng ID cũ.
6. Private keys và message keys ở browser: IndexedDB và bộ nhớ của tab sở hữu. API, PostgreSQL, Redis và gateway chỉ nhận public keys, envelope và metadata; không nhận plaintext của thread E2EE.
7. Crypto duy nhất nằm trong Go `internal/e2ee`, được build Go/WASM với bridge `syscall/js`. Không viết một bản X3DH/KDF/AEAD/chữ ký bằng JavaScript.
8. JavaScript giữ luồng đang có: REST gửi message; WebSocket nhận message mới; REST lấy lịch sử khi mở thread, tải trang cũ và lấy bù sau reconnect/gap. Không chuyển sang polling hoặc thêm client ngoài browser.
9. Dùng các bảng PostgreSQL hiện có. Transaction PostgreSQL tiêu thụ OPK public; transaction IndexedDB xử lý state local. Không dùng Redis lock/reservation cho OPK.
10. Giữ handler → service → repository. Bridge chỉ làm crypto/encoding, không gọi HTTP, giữ JWT, truy cập IndexedDB hoặc sửa DOM. Không thêm framework, event bus, generic repository, worker nền hoặc một tầng service mới cho toàn bộ ứng dụng.

### 2. Sequence diagram: web REST, WebSocket và Go/WASM

Hai người đăng nhập bằng web hiện có. Tab sở hữu khóa Web Locks mới được khởi tạo/phục hồi state local và dùng E2EE. Mỗi người lưu private keys trước upload public bundle. B có thể offline sau đăng ký; ciphertext vẫn được lưu trong PostgreSQL.

```mermaid
sequenceDiagram
    participant A as Web A + Go/WASM
    participant LA as IndexedDB A
    participant API as chat-api
    participant DB as PostgreSQL
    participant R as Redis Stream
    participant G as ws-gateway
    participant B as Web B + Go/WASM
    participant LB as IndexedDB B

    Note over A,LA: Sau đăng nhập, lấy Web Lock theo API URL và user UUID
    A->>A: Go/WASM generateKeyPair và signPrekey cho lần khởi tạo
    A->>LA: Commit private keys, signature và pending_upload
    A->>API: POST /e2ee/prekeys (chỉ public bundle)
    API->>DB: Transaction khóa user và lưu public keys
    API-->>A: 200 OK sau commit
    A->>LA: Commit xác nhận đăng ký và bỏ pending_upload
    Note over B,LB: B cũng lấy Web Lock, khôi phục hoặc khởi tạo local
    B->>B: Go/WASM tạo và ký keys nếu khởi tạo lần đầu
    B->>LB: Commit private keys và pending_upload trước HTTP
    B->>API: POST /e2ee/prekeys (chỉ public bundle)
    API->>DB: Transaction lưu public keys của B
    API-->>B: 200 OK sau commit
    B->>LB: Commit xác nhận đăng ký
    B->>G: WebSocket qua ticket hiện có khi online
    A->>API: POST /threads/direct (encryption_mode=e2ee)
    API->>DB: Transaction khóa hai user, kiểm tra IK/SPK, tạo hoặc mở direct
    API-->>A: Thread ID và mode
    A->>API: POST /e2ee/bundles/:user_id/claim (B, thread_id)
    rect rgb(235, 245, 255)
        Note over API,DB: Transaction PostgreSQL consume OPK public
        API->>DB: BEGIN, kiểm tra direct E2EE và hai participant active
        API->>DB: Khóa user B, đọc IK/SPK, DELETE RETURNING OPK nhỏ nhất
        DB-->>API: Bundle public, OPK hoặc null
        API->>DB: COMMIT
        DB-->>API: Commit thành công
    end
    API-->>A: Bundle public sau commit
    A->>A: Go/WASM verifyBundle và seal: EK/nonce mới, X3DH, encrypt với AAD
    A->>LA: Commit message key, hash, peer pin và nguyên pending POST body
    A->>API: POST /threads/:id/messages (REST, UUID, e2ee_v1, content string)
    API->>DB: Transaction khóa thread, membership, retry/mode/header, seq và insert
    DB-->>API: Ciphertext đã commit
    API->>R: XADD message.created chứa ciphertext opaque
    Note over API,R: XADD lỗi sau commit có thể trả 503, sender giữ pending để retry
    API-->>A: Response message sau publish được xác nhận
    A->>LA: Bỏ pending khi response khớp và local commit thành công
    A->>A: Go/WASM decryptWithKey từ key đã lưu và hiển thị own message
    R->>G: Event với recipient snapshot hiện có
    alt B đang online
        G-->>B: WebSocket message.created chứa envelope
    else B offline hoặc đã bỏ lỡ event
        Note over G,B: Gateway không giữ khóa và không giải mã
        B->>G: Kết nối lại WebSocket bằng ticket hiện có
        B->>API: GET /threads/:id/messages khi mở thread hoặc reconnect thành công
        API->>DB: Đọc lịch sử theo quyền và cursor
        API-->>B: Messages chứa envelope và outer metadata
    end
    opt Reconnect hoặc gap seq cần lấy bù
        B->>API: GET history qua before_seq/next_cursor tới mốc đã biết
        API-->>B: Messages REST, có thể trùng UUID với event
    end
    B->>B: Gộp UUID, kiểm tra context/hash, Go/WASM open hoặc decryptWithKey
    B->>LB: Với message mới, transaction cache key/context/hash/pin và xóa OPK private
    LB-->>B: Local commit thành công
    B->>B: Render plaintext bằng textContent theo seq
    B->>API: PUT /threads/:id/read chỉ sau decrypt, local commit và render hợp lệ
    Note over B,LB: AEAD hoặc lưu local lỗi: giữ OPK, không render plaintext và không vượt read marker
    Note over A,B: B gửi lại bằng bundle A và lượt X3DH mới; cùng pipeline web với vai trò đổi chỗ
```

Luồng publish thành công được vẽ ở trên; khi query snapshot/XADD lỗi sau commit, response có thể là 503 và sender retry nguyên body. Sender nhận own message qua HTTP response/lịch sử, không phụ thuộc gateway gửi lại cho sender. Cùng một pipeline giải mã xử lý event và REST; cached key tránh consume OPK lần nữa khi event trùng hoặc reload lịch sử.

### 3. Crypto và encoding

| Tham số | Giá trị cố định |
| --- | --- |
| DH | X25519, dùng `crypto/ecdh` |
| KDF | HKDF-SHA-256, dùng `crypto/hkdf` |
| HKDF input | 32 byte `0xFF` nối các DH outputs theo thứ tự bên dưới |
| HKDF salt | 32 byte zero |
| HKDF info | ASCII `Mini-Hermes/X3DH/v1` |
| Secret / AES key | 32 byte |
| AEAD | AES-256-GCM, dùng `crypto/aes`, `crypto/cipher` |
| Nonce | 12 byte từ `crypto/rand`, sinh một lần khi tạo message |
| Public/private key X25519 | 32 byte raw |
| Public key encoding để ký và làm AAD | `0x05` nối public key raw 32 byte |
| Chữ ký SPK | 64 byte, ký encoding 33 byte của SPK bằng identity private key |
| Thư viện chữ ký | Chỉ import `go.mau.fi/libsignal/ecc`, pin module `go.mau.fi/libsignal v0.2.2` |
| Encoding JSON | Base64 chuẩn có padding: `base64.StdEncoding` |

**Lưu ý về chữ ký:** package `ecc` dùng biến thể chữ ký Curve25519 của libsignal, truyền sign bit trong signature. Không mô tả nó là byte-for-byte XEdDSA canonical của bản đặc tả 2016. Profile demo cố định vào thư viện này, giữ một identity key Curve25519 để DH và kiểm tra chữ ký; không thay bằng một identity Ed25519 riêng. Không tuyên bố tương thích toàn bộ wire protocol của Signal. Dependency có license GPL-3.0; ghi rõ phiên bản và license trong tài liệu dependency.

Code không tự viết phép toán đường cong hoặc thuật toán chữ ký. Không gọi `ecc.CreateKeyPair`, không bật debug logging của thư viện. Khôi phục private key bằng `crypto/ecdh.X25519().NewPrivateKey`; wrapper ký dùng `ecc.NewDjbECPrivateKey`, wrapper kiểm tra dùng `ecc.NewDjbECPublicKey` và signature `[64]byte` đã kiểm tra độ dài.

DH theo chiều gửi:

```text
DH1 = X25519(IK_sender_private, SPK_recipient_public)
DH2 = X25519(EK_sender_private, IK_recipient_public)
DH3 = X25519(EK_sender_private, SPK_recipient_public)
DH4 = X25519(EK_sender_private, OPK_recipient_public)  // chỉ khi có OPK

SK = HKDF-SHA-256(FF32 || DH1 || DH2 || DH3 [|| DH4], zero32, info, 32)
```

DH theo chiều nhận, giữ nguyên thứ tự output:

```text
DH1 = X25519(SPK_recipient_private, IK_sender_public)
DH2 = X25519(IK_recipient_private, EK_sender_public)
DH3 = X25519(SPK_recipient_private, EK_sender_public)
DH4 = X25519(OPK_recipient_private, EK_sender_public)  // chỉ khi header có OPK ID
```

Nếu không có OPK thì dùng 3DH; không tự chèn DH4 bằng zero. Nếu header có OPK ID nhưng client không còn private key và cũng chưa cache message key, giải mã thất bại; không chuyển sang 3DH để thử.

Kiểm tra signature trước khi tính secret. Mọi lỗi DH, HKDF, nonce hoặc AEAD đều dừng lượt xử lý. Không hiển thị plaintext và không xóa OPK khi AEAD thất bại.

### 4. API public prekeys

Tất cả endpoint dưới đây yêu cầu JWT. Actor luôn lấy từ JWT, không nhận `user_id` để chọn chủ sở hữu ở upload.

#### Đăng ký hoặc bổ sung OPK

`POST /e2ee/prekeys`

```json
{
  "identity_public_key": "<base64 của 32 byte>",
  "signed_prekey": {
    "key_id": 1,
    "public_key": "<base64 của 32 byte>",
    "signature": "<base64 của 64 byte>"
  },
  "one_time_prekeys": [
    {"key_id": 2, "public_key": "<base64 của 32 byte>"},
    {"key_id": 3, "public_key": "<base64 của 32 byte>"}
  ]
}
```

Response `200 OK`:

```json
{
  "user_id": "<external user UUID>",
  "identity_public_key": "<base64 của 32 byte>",
  "signed_prekey_id": 1,
  "last_prekey_id": 3,
  "one_time_prekey_count": 2
}
```

Quy tắc:

- Body tối đa 16 KiB; quá giới hạn trả `413`. Mỗi request có 0–100 OPK. Public key đúng 32 byte, signature đúng 64 byte; Base64 phải canonical. Từ chối JSON có trường lạ, dữ liệu thừa sau JSON, ID không dương hoặc ID trùng trong request, kể cả trùng SPK với OPK. Decoder strict áp dụng riêng endpoint mới, không bật một cấu hình Gin toàn cục làm thay đổi API cũ.
- Kiểm tra public key low-order bằng X25519 ECDH với một private key kiểm tra cố định, không phải private key của người dùng; từ chối nếu ECDH lỗi. Không chỉ kiểm tra độ dài vì `NewPublicKey` của X25519 chưa loại low-order key.
- Kiểm tra signature của SPK với identity public key trước khi ghi DB.
- Lần đầu lưu identity, một SPK và các OPK trong cùng transaction, khóa dòng user trước khi đọc/sửa.
- Đã đăng ký thì identity và bộ SPK gồm ID/public key/signature phải giống bộ đã lưu. Khác trả `409`; không thay khóa âm thầm.
- `users.last_prekey_id` là **mốc ID cao nhất đã cấp**, dùng chung namespace SPK/OPK; không phải số OPK còn lại.
- OPK mới chỉ được insert khi ID lớn hơn watermark ở đầu transaction. Update watermark bằng ID lớn nhất đã đăng ký; không giảm.
- Với ID không vượt watermark: nếu OPK còn trong DB thì public key phải khớp; nếu row đã bị claim/xóa thì bỏ qua, không insert lại. Đây là hành vi retry upload sau khi có người claim, không khôi phục OPK đã dùng. Không còn row thì không thể so sánh lại public key cũ; không tuyên bố có tombstone từng key.
- Signature của SPK được client sinh và lưu một lần, không ký lại khi retry upload.
- Việc upload/refill và claim cùng khóa dòng user nên không tranh chấp watermark và OPK.

#### Lấy một bundle và tiêu thụ OPK

`POST /e2ee/bundles/:user_id/claim`

Body:

```json
{"thread_id": "<external thread UUID>"}
```

Response `200 OK`:

```json
{
  "user_id": "<recipient external UUID>",
  "identity_public_key": "<base64 của 32 byte>",
  "signed_prekey": {
    "key_id": 1,
    "public_key": "<base64 của 32 byte>",
    "signature": "<base64 của 64 byte>"
  },
  "one_time_prekey": {"key_id": 2, "public_key": "<base64 của 32 byte>"}
}
```

Khi hết OPK, `one_time_prekey` là `null`; không bỏ field và không trả key đã dùng.

Repository kiểm tra thread là direct E2EE và actor/recipient là hai participant active. Không claim cho chính mình hoặc cho user ngoài thread. Mode không hợp lệ trả `409`; không có quyền trả `403`; user/thread/bundle không tồn tại trả `404`.

Trong một transaction: kiểm tra quyền → khóa dòng recipient user → đọc IK/SPK → lấy OPK nhỏ nhất và DELETE RETURNING → commit → trả bundle. Các lượt claim cùng user được serialize bằng khóa user; không cần thêm Redis lock, reservation hay `SKIP LOCKED`.

Một OPK chỉ xuất hiện trong một response claim đã commit. Nếu response bị mất sau commit hoặc sender bỏ lượt gửi, OPK đó bị tiêu thụ nhưng không tạo message. Chấp nhận đánh đổi này; claim không có cơ chế hoàn trả key hoặc idempotency riêng. Receiver giữ OPK private cho tới khi nhận/decrypt message tương ứng.

Upload/claim không ghi private key. Không nhận secret, message key, ephemeral private key hay plaintext để server mã hóa hộ.

### 5. Thread và đường gửi message

Mở hoặc tạo direct:

```http
POST /threads/direct
```

```json
{"peer_id": "<peer external UUID>", "encryption_mode": "e2ee"}
```

`encryption_mode` là field tùy chọn, chỉ chấp nhận `plaintext` hoặc `e2ee` khi có mặt. DTO dùng `*string` để phân biệt không gửi field với một yêu cầu mode rõ ràng.

- Thread chưa có: không gửi mode thì tạo plaintext; mode E2EE thì cả hai tài khoản phải có identity và SPK đã đăng ký. Thiếu bundle trả `409`.
- Thread đã có: không gửi mode thì trả thread cùng mode thực tế; gửi mode rõ ràng và khác mode hiện tại thì `409`.
- Không tạo thread thứ hai cùng cặp để tránh conflict, không đổi mode của thread hiện có, không chuyển lịch sử cũ.
- Response tạo/mở thread và từng item của `GET /threads` bổ sung `encryption_mode`.
- Duy trì khóa hai user theo thứ tự internal ID khi tạo/mở direct. Kiểm tra public key/SPK trong transaction đó.

Gửi ciphertext vẫn dùng endpoint message hiện tại:

```json
{
  "message_id": "<UUID do client sinh>",
  "content_format": "e2ee_v1",
  "content": "<một chuỗi JSON envelope, đã escape theo JSON outer body>"
}
```

`content` là **string**, không đổi thành object. Envelope được marshal một lần và chuỗi đó được lưu nguyên vào `messages.content`. Không tách ciphertext vào `metadata` hoặc thêm cột.

| Thread | Format hợp lệ cho text |
| --- | --- |
| Direct plaintext | `plaintext` |
| Group plaintext | `plaintext` |
| Direct E2EE | `e2ee_v1` |

Format bị bỏ trống/không gửi tương đương `plaintext` để giữ client cũ. Format khác danh sách trả `400`; format không khớp mode trả `409`. System message nhóm giữ nghiệp vụ và format hiện có.

Plaintext giữ trim và giới hạn 1–1000 ký tự hiện tại. E2EE không trim, không sửa hay marshal lại content ở server. Envelope tối đa 8 KiB; outer body tối đa 16 KiB. Client cũng kiểm tra plaintext 1–1000 Unicode rune, UTF-8 hợp lệ và không NUL trước khi encrypt. Ciphertext sau decode phải dài 17–4016 byte, gồm GCM tag 16 byte.

Trong transaction gửi tin: khóa thread → kiểm tra membership active → xử lý UUID retry → kiểm tra mode và header với dữ liệu DB → cấp seq → insert message cùng content_format → commit. Khi retry hợp lệ, trả message cũ; không cấp seq hoặc kiểm tra lại OPK đã bị claim.

Repository kiểm tra header `recipient_id` là peer active; `sender_identity_key` khớp identity public key của actor; SPK ID khớp SPK ổn định của recipient. OPK có thể đã bị xóa lúc claim, nên không yêu cầu còn row OPK mới cho lưu message/retry. Không thể dùng các kiểm tra này để xác nhận ciphertext sẽ giải mã thành công.

Điều kiện retry: cùng message UUID, sender, thread, kind, content_format và **content string đúng byte**. Thay đổi bất kỳ phần nào trả conflict chung `409`, không trả dữ liệu message của người khác.

Service và repository `Send` thêm argument `contentFormat string` trước `content string`. Cập nhật handler interface, mock và tất cả call sites. SQL insert không hardcode `plaintext`. Response, event và lịch sử trả nguyên format/content như đã lưu.

Lỗi Redis sau DB commit vẫn có thể trả `503` cùng message ID/seq như hiện tại. Web giữ pending và retry đúng payload; không mã hóa lại để “sửa” lỗi publish. Khoảng hở commit → XADD hiện có không được giải quyết bằng cách viết thêm outbox trong tuần này.

### 6. Envelope và AAD

Envelope khi nhìn bên trong chuỗi `content`:

```json
{
  "version": 1,
  "recipient_id": "<recipient external UUID>",
  "sender_identity_key": "<base64 raw 32 byte>",
  "ephemeral_key": "<base64 raw 32 byte>",
  "signed_prekey_id": 1,
  "one_time_prekey_id": 2,
  "nonce": "<base64 raw 12 byte>",
  "ciphertext": "<base64 ciphertext nối GCM tag>"
}
```

Với 3DH: `one_time_prekey_id` phải là `null`. ID prekey là số nguyên dương trong miền `int64`; giá trị 0 chỉ dùng bên trong AAD để biểu diễn không có OPK. Khi có OPK, ID phải khác SPK ID. Parser từ chối field lạ, version khác 1, UUID không canonical/non-zero, thiếu field bắt buộc, Base64 không canonical và kích thước sai.

`MessageContext` lấy từ outer message và thread đang mở: thread UUID, message UUID, sender UUID, recipient UUID. `recipient_id` trong envelope phải khớp context. Không dùng ID lấy tùy ý từ envelope thay cho context khi decrypt.

AAD có byte layout cố định; không dùng JSON marshal hoặc nối UUID dạng text:

```text
Encode(IK_sender) || Encode(IK_recipient)
|| ASCII("Mini-Hermes/e2ee_v1") || 0x00
|| UUID16(thread_id) || UUID16(message_id)
|| UUID16(sender_id) || UUID16(recipient_id)
|| Encode(EK_sender)
|| uint64_big_endian(signed_prekey_id)
|| uint64_big_endian(one_time_prekey_id_or_zero)
```

`Encode(public)` là `0x05` + raw 32 byte; UUID16 là 16 byte của UUID đã parse. Sender và receiver dùng cùng hàm tạo AAD. Không bind `seq`/`created_at` vì server cấp chúng sau khi encrypt.

AAD ràng buộc ciphertext với tài khoản, chiều gửi, thread, UUID và prekey header. Đổi các giá trị này sẽ làm AEAD kiểm tra thất bại. Server vẫn nhìn thấy chúng.

### 7. Package dùng chung và chữ ký hàm

Tạo package `internal/e2ee`, alias import là `e2ee`. Package không phụ thuộc Gin, PostgreSQL, Redis, JWT hoặc IndexedDB. JSON wire types dùng chung giữa API và client Go/WASM, không viết hai bộ envelope riêng.

```go
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
    SignedPrekey      SignedPrekey    `json:"signed_prekey"`
    OneTimePrekeys    []PublicPrekey  `json:"one_time_prekeys"`
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
    SignedPrekey      SignedPrekey   `json:"signed_prekey"`
    OneTimePrekey     *PublicPrekey  `json:"one_time_prekey"`
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

func GenerateKeyPair() (KeyPair, error)
func ValidatePublicKey(public [32]byte) error
func SignPrekey(identityPrivate, signedPublic [32]byte) ([64]byte, error)
func VerifyBundle(bundle Bundle) error
func ParseEnvelope(content string) (Envelope, error)
func EncodeEnvelope(envelope Envelope) (string, error)
func Seal(ctx MessageContext, identity KeyPair, recipient Bundle,
    plaintext []byte) (Envelope, [32]byte, error)
func Open(ctx MessageContext, identity, signed KeyPair, oneTime *KeyPair,
    envelope Envelope) ([]byte, [32]byte, error)
func DecryptWithKey(ctx MessageContext, senderIK, recipientIK, key [32]byte,
    envelope Envelope) ([]byte, error)
```

Hàm trả `[32]byte` từ Seal/Open là message key để client lưu. `Open` được truyền SPK/OPK đã chọn đúng ID từ state; client phải kiểm tra lựa chọn đó. `DecryptWithKey` dành cho lịch sử hoặc message đã xử lý, không tạo thêm DH hay consume OPK lần nữa.

Các helper decode Base64/AAD/DH/KDF có thể là hàm private trong package. Được thêm helper nhỏ khi API cần decode đúng cùng quy tắc; không đổi chữ ký hàm công khai trên hoặc tạo interface cho AES/DH chỉ để mock.

Không marshal `KeyPair` vào HTTP body; chỉ các public wire structs được dùng cho API. DTO local của bridge và profile IndexedDB có struct riêng với private keys; chúng không phải wire types HTTP.

#### Bridge Go/WASM: hợp đồng JSON local cố định

Entry point `cmd/e2ee-wasm/main_js.go`, build constraint `//go:build js && wasm`. Import `internal/e2ee` và `syscall/js`; đăng ký object `globalThis.MiniHermesE2EE` với đúng sáu method bên dưới. Mỗi method nhận **một JSON string** và trả **một JSON string** theo envelope kết quả này:

```json
{"ok": true, "data": {"signature": "<base64 của 64 byte>"}, "error": null}
```

Ví dụ thành công trên là của signPrekey; `data` của các method khác dùng đúng object bên dưới, không có wrapper trung gian. Khi lỗi, luôn trả:

```json
{"ok": false, "data": null, "error": {"code": "invalid_input", "message": "Yêu cầu crypto không hợp lệ."}}
```

`ok` là boolean; `error` thành công là null, lỗi là object có `code`/`message`. Mã lỗi cố định: `invalid_input`, `invalid_key`, `invalid_bundle`, `invalid_context`, `decrypt_failed`, `crypto_failed`. Message là câu mô tả an toàn, không chứa request, private key, message key hoặc plaintext. Chữ ký bundle sai trả `ok:false`/`invalid_bundle`, không trả `valid:false` dưới `ok:true`.

Decoder riêng bridge: đúng một argument string, JSON object strict, từ chối field lạ/thiếu, dữ liệu thừa, key/nonce/signature sai encoding/length, keypair public không khớp private. Input bridge tối đa 32 KiB UTF-8; content vẫn tối đa 8 KiB theo II.5–6. Không bật decoder setting toàn cục. Lỗi input/crypto không panic thoát WASM; callback trả lỗi an toàn. Bridge không lưu keys/payload giữa các lần gọi và không log input/output.

**DTO local** dùng Base64 chuẩn có padding của **raw 32 byte** (`base64.StdEncoding`), không phải `0x05 + public32` và không dùng Base64 bỏ padding. Đây là boundary local JavaScript ↔ WASM, **không được dùng làm HTTP DTO**:

```json
{
  "private_key": "<base64 raw 32 byte>",
  "public_key": "<base64 raw 32 byte>"
}
```

Context local có cùng tên snake_case với metadata message, ánh xạ sang `e2ee.MessageContext` mà không đổi hàm Go:

```json
{
  "thread_id": "<canonical non-zero thread UUID>",
  "message_id": "<canonical non-zero message UUID>",
  "sender_id": "<canonical non-zero sender UUID>",
  "recipient_id": "<canonical non-zero recipient UUID>"
}
```

Các placeholder dưới đây là mô tả kiểu, chưa phải fixture crypto hợp lệ. `bundle` dùng nguyên `Bundle` tại II.7/API II.4, kể cả `one_time_prekey:null`. Key ID của API vẫn int64 dương; code web chỉ tạo/nhận số nguyên biểu diễn chính xác (`Number.isSafeInteger`), dừng nếu vượt miền đó, không round/truncate hoặc đổi API thành ID string.

**generateKeyPair**

Input tạo mới:
```json
{}
```

Input khôi phục/validate keypair local khi load (không sinh private mới):
```json
{"private_key": "<base64 raw 32 byte đã lưu>"}
```

Data thành công:
```json
{
  "key_pair": {"private_key": "<base64 raw 32 byte>", "public_key": "<base64 raw 32 byte>"},
  "fingerprint": "<64 ký tự SHA-256 hex lowercase của public raw 32 byte>"
}
```

Input {} gọi `e2ee.GenerateKeyPair`. Input có private_key dùng `crypto/ecdh.X25519().NewPrivateKey` để khôi phục và derive public; không gọi RNG hoặc ghi state. Web so public trả về với public đã lưu, khác thì missing_keys/error, không ghi đè. Fingerprint dùng helper SHA-256 Go. Dùng cùng method cho IK/SPK/OPK; chỉ fingerprint IK được hiển thị cho người dùng.

**signPrekey**

Input:
```json
{
  "identity_private_key": "<base64 raw 32 byte>",
  "signed_prekey_public_key": "<base64 raw 32 byte>"
}
```

Data thành công:
```json
{"signature": "<base64 của 64 byte>"}
```

Gọi `e2ee.SignPrekey`; dữ liệu được ký vẫn là `0x05 + SPK_public32`, không ký JSON hay raw32 trực tiếp. Sinh/lưu signature một lần; retry upload dùng signature cũ. Giữ nguyên ghi chú biến thể libsignal tại II.3.

**verifyBundle**

Input:
```json
{
  "bundle": {
    "user_id": "<recipient external UUID>",
    "identity_public_key": "<base64 raw 32 byte>",
    "signed_prekey": {"key_id": 1, "public_key": "<base64 raw 32 byte>", "signature": "<base64 của 64 byte>"},
    "one_time_prekey": null
  }
}
```

Data thành công:
```json
{"valid": true, "fingerprint": "<64 ký tự SHA-256 hex lowercase của identity public raw 32 byte>"}
```

Gọi `e2ee.VerifyBundle`. Kiểm tra user/context và TOFU pin vẫn thuộc controller web; bridge không có state để biết pin. Có thể dựng bundle public của chính mình từ local IK/SPK/signature, OPK null, để đối chiếu fingerprint khi load; không claim OPK chỉ để hiển thị fingerprint.

**seal**

Input:
```json
{
  "context": {"thread_id": "<thread UUID>", "message_id": "<message UUID>", "sender_id": "<sender UUID>", "recipient_id": "<recipient UUID>"},
  "identity": {"private_key": "<base64 raw 32 byte>", "public_key": "<base64 raw 32 byte>"},
  "bundle": {
    "user_id": "<recipient external UUID>",
    "identity_public_key": "<base64 raw 32 byte>",
    "signed_prekey": {"key_id": 1, "public_key": "<base64 raw 32 byte>", "signature": "<base64 của 64 byte>"},
    "one_time_prekey": {"key_id": 2, "public_key": "<base64 raw 32 byte>"}
  },
  "plaintext": "<chuỗi UTF-8 đã kiểm tra và trim>"
}
```

Data thành công:
```json
{
  "content": "<chuỗi JSON envelope do Go EncodeEnvelope marshal đúng một lần>",
  "message_key": "<base64 raw 32 byte>",
  "content_sha256": "<64 ký tự SHA-256 hex lowercase của bytes UTF-8 content>"
}
```

Adapter chuyển plaintext string sang []byte, gọi `e2ee.Seal`, rồi `e2ee.EncodeEnvelope` một lần và tính hash bằng Go. Kiểm tra lại plaintext 1–1000 rune, UTF-8 và không NUL trong Go trước crypto. Không trả/lưu EK private. Web sử dụng nguyên content string trong outer POST body, không stringify envelope lại. Mỗi lần Seal sinh EK/nonce mới; retry không được gọi method này.

**open**

Input:
```json
{
  "context": {"thread_id": "<thread UUID>", "message_id": "<message UUID>", "sender_id": "<sender UUID>", "recipient_id": "<recipient UUID>"},
  "identity": {"private_key": "<base64 raw 32 byte>", "public_key": "<base64 raw 32 byte>"},
  "signed_prekey": {"private_key": "<base64 raw 32 byte>", "public_key": "<base64 raw 32 byte>"},
  "one_time_prekey": {"private_key": "<base64 raw 32 byte>", "public_key": "<base64 raw 32 byte>"},
  "content": "<nguyên content string từ event hoặc REST>"
}
```

Data thành công:
```json
{
  "plaintext": "<chuỗi UTF-8 hợp lệ>",
  "message_key": "<base64 raw 32 byte>",
  "content_sha256": "<64 ký tự SHA-256 hex lowercase của bytes UTF-8 content>",
  "sender_fingerprint": "<64 ký tự SHA-256 hex lowercase của sender identity public raw 32 byte>"
}
```

Bridge gọi `e2ee.ParseEnvelope`, rồi `e2ee.Open`, kiểm tra plaintext trước khi trả. Web chọn SPK/OPK private đúng ID từ IndexedDB; bridge không lookup state. Với 3DH, `one_time_prekey:null` bắt buộc; header có OPK ID nhưng private thiếu thì dừng, không fallback. Kết quả này chưa được phép render: web phải commit cache key/context/hash/pin và xóa OPK private trong cùng transaction local trước.

**decryptWithKey**

Input:
```json
{
  "context": {"thread_id": "<thread UUID>", "message_id": "<message UUID>", "sender_id": "<sender UUID>", "recipient_id": "<recipient UUID>"},
  "sender_identity_key": "<base64 raw 32 byte>",
  "recipient_identity_key": "<base64 raw 32 byte>",
  "message_key": "<base64 raw 32 byte>",
  "expected_content_sha256": "<64 ký tự SHA-256 hex lowercase đã lưu>",
  "content": "<nguyên content string từ event hoặc REST>"
}
```

Data thành công:
```json
{"plaintext": "<chuỗi UTF-8 hợp lệ>"}
```

Go tính hash content và so với expected hash trước AEAD; sai trả `invalid_context`. Web so context hiện tại với context cache trước khi gọi. Bridge ParseEnvelope rồi gọi `e2ee.DecryptWithKey`; kiểm tra plaintext hợp lệ. Không DH, claim bundle hay xóa OPK lại. Hash/fingerprint chỉ là helper SHA-256 Go cho local cache/UI; không đổi AAD, KDF hoặc API public.

#### Build và runtime browser

- `make wasm` build với `GOOS=js GOARCH=wasm go build -o web/e2ee.wasm ./cmd/e2ee-wasm`; copy `wasm_exec.js` từ `$(go env GOROOT)/lib/wasm/wasm_exec.js` của **cùng compiler/phiên bản Go** vừa build sang `web/wasm_exec.js`. Giữ Go version repo; không tải runtime từ CDN hoặc chép runtime khác phiên bản. Trên Windows dùng lệnh PowerShell tương đương khi Make không chạy.
- Hai file `web/e2ee.wasm`, `web/wasm_exec.js` là artifact sinh từ build, thêm đường dẫn vào .gitignore ở Prompt 1. Không chứa state, JWT hay keys người dùng và không commit binary/runtime. `make build` native không thay `make wasm`.
- Router phục vụ riêng `/e2ee.wasm` (`application/wasm`) và `/wasm_exec.js` như asset; cache policy no-store tương tự web hiện có cho demo. Prompt 1 thêm hai route asset để có thể kiểm chứng bridge; Prompt 2 thêm endpoint public prekeys. Không mở rộng thành static directory chứa dữ liệu local.
- Prompt 4 thêm loader `web/e2ee-wasm.js`. Loader dùng runtime `Go`, fetch/instantiate artifact rồi gọi `go.run`; chờ bridge đăng ký đủ sáu method trước khi báo sẵn sàng. Không chờ promise `go.run` kết thúc để coi runtime sẵn sàng vì Go main phải giữ callbacks sống. Runtime giữ tham chiếu `js.Func`, main không thoát khi đang phục vụ; crypto callbacks không gọi HTTP/DOM/IndexedDB.
- Fetch asset thuộc loader JavaScript, không thuộc bridge Go. Loader lỗi/missing artifact/runtime mismatch chặn E2EE và hiện trạng thái lỗi; plaintext/group vẫn hoạt động. Không tự fallback sang crypto JavaScript.
- Native tests cho crypto và build js/wasm là hai bằng chứng riêng; chỉ build thành công chưa chứng minh bridge chạy trong browser. Browser bridge smoke dùng script tạm, xóa sau kiểm chứng theo AGENTS.md.

### 8. IndexedDB, ownership, retry và state web

#### Scope local và quyền sở hữu

REST/JWT và WebSocket ticket giữ ở JavaScript hiện có. JWT tiếp tục ở sessionStorage theo tab; không đưa JWT/password vào IndexedDB hoặc bridge. Đọc sub ở client chỉ để bind external UUID, không thay việc API xác minh JWT; kiểm tra user UUID khớp GET /users, response upload và profile local.

`api_url` là absolute base URL của REST client hiện tại, normalize bỏ trailing slash, không query/fragment. Demo dùng web và API cùng origin; không thêm màn hình cấu hình backend. IndexedDB `mini-hermes-e2ee`, version 1, object store `profiles`, keyPath `["api_url", "user_id"]`. Mỗi record là một profile state; không có plaintext trong record. Không fallback sang localStorage/sessionStorage hoặc server để lưu keys.

Trước đọc private keys hoặc sửa profile, yêu cầu Web Lock exclusive có name `"mini-hermes:e2ee:" + JSON.stringify([api_url, user_id])`, với `ifAvailable:true`. Tab lấy được lock giữ callback/promise sống trong toàn bộ phiên E2EE. Tab không lấy được hiển thị `other_tab`, không đọc private profile, không generate/upload/refill/claim/send/retry/decrypt hay tự PUT read E2EE; vẫn dùng plaintext/group và thấy metadata/placeholder E2EE. Có nút thử tiếp quản sau khi tab sở hữu đóng/đăng xuất; không dùng steal lock, vòng dò lock định kỳ hoặc chia sẻ khóa/ plaintext qua tab.

Trong tab sở hữu, một hàng Promise serialize toàn bộ thao tác thay đổi state: khởi tạo, upload/refill, send/retry/cancel, nhận event/REST và save. Không thêm hai goroutine/worker cùng ghi hoặc một lớp đồng bộ nhiều thiết bị. Lỗi một task được xử lý và không làm đứt vĩnh viễn hàng; task sau phải kiểm tra trạng thái và phiên trước khi tiếp tục.

Web Locks chỉ điều phối cùng origin trong cùng browser/profile; không khóa giữa browser profiles, origin hoặc thiết bị khác. Điều kiện demo là một profile/client cho mỗi tài khoản; Alice/Bob có scope UUID riêng. Thiếu Web Locks, IndexedDB, WebAssembly hoặc secure context thì báo lỗi và chặn E2EE, không dùng mutex chỉ ở RAM để giả lập bảo đảm nhiều tab. HTTPS khi triển khai; localhost chỉ dùng khi browser coi là secure context. [Web Locks API](https://developer.mozilla.org/en-US/docs/Web/API/Web_Locks_API).

Đăng xuất/đổi tài khoản: chặn task mới, hủy HTTP qua sessionAbort hiện có, bỏ callback cũ theo sessionVersion/scope, xóa keys/plaintext/draft khỏi RAM và DOM, chờ transaction local đã mở kết thúc rồi nhả lock. Không xóa profile IndexedDB để đăng xuất. Task của tài khoản cũ không được sửa profile/DOM của tài khoản mới; lock của scope mới phải được lấy riêng.

#### Profile state tối thiểu

| Dữ liệu | Quy tắc |
| --- | --- |
| version=1, api_url, user_id | Validate khi load; sai scope/version dừng, không reset |
| identity: DTO keypair local | Sinh một lần; public/private phải khớp; fingerprint của IK được lưu/đối chiếu |
| signed_prekey: ID, DTO keypair, signature | ID 1 lúc đầu; cố định trong demo; signature Base64 64 byte sinh/lưu một lần |
| one_time_prekeys theo ID, next_prekey_id | Batch 20, ID đầu 2, tăng đơn điệu; giữ private tới decrypt và local commit thành công |
| registration và last_prekey_id | Chỉ xác nhận đăng ký sau response 200 khớp và local commit; watermark server không giảm; lưu trạng thái cần upload |
| pending_upload | Nguyên public UploadRequest và nguyên outer body JSON string; private/signature đã commit trước HTTP |
| peer_pins theo user UUID | Identity public Base64 và fingerprint hex; TOFU lần đầu, đổi key dừng |
| message_keys theo message UUID | Key Base64 raw32, context snake_case, content_sha256 và sender/recipient identity public keys dùng AAD |
| Một pending_send cho toàn profile | Thread/context, UUID, content_format, nguyên content string và nguyên outer POST body string |

Keypair/private chỉ có trong DTO local; public request được dựng từ wire types API, không serialize cả profile/DTO keypair vào HTTP. Profile không chứa JWT/password, EK private, plaintext hoặc bản draft plaintext. Draft và plaintext đã giải mã chỉ ở RAM/DOM của phiên đăng nhập; reload phải giải mã lại từ cached key.

Mỗi save dùng transaction IndexedDB readwrite trên profiles, commit toàn bộ thay đổi liên quan bằng một lần put. Tính crypto và chờ HTTP **ngoài** transaction IndexedDB; không mở transaction rồi await HTTP/Promise crypto làm transaction inactive. Chỉ công nhận save sau `transaction.oncomplete`, không dùng `request.onsuccess` làm commit. Khi abort/error/quota, giữ state đã commit trước đó, không cập nhật RAM như thành công; reload committed profile nếu cần. Không tuyên bố local transaction tương đương bảo đảm chống mất điện, storage eviction hoặc khôi phục khóa. [Using IndexedDB](https://developer.mozilla.org/en-US/docs/Web/API/IndexedDB_API/Using_IndexedDB), [IDBTransaction](https://developer.mozilla.org/en-US/docs/Web/API/IDBTransaction).

#### Khởi tạo/khôi phục và đăng ký public bundle

Sau đăng nhập/khôi phục session: hiện initializing, load WASM, lấy lock và load profile. Profile hợp lệ được khôi phục với đúng IK/SPK/pending; dùng generateKeyPair với private_key đã lưu để derive/so public cho IK/SPK/OPK, không sinh private mới hoặc ghi đè. Nếu thiếu profile, thiếu private key hoặc state malformed thì hiện missing_keys. Không thể suy ra account chưa từng đăng ký từ việc không có state local; API cố định không có endpoint đọc trạng thái đăng ký không consume OPK.

Thao tác `Khởi tạo E2EE cho tài khoản mới` là hành động rõ trên web khi chưa có profile và người dùng biết account chưa đăng ký. Sinh IK/SPK/20 OPK, ký SPK bằng bridge, lưu private + signature + next ID + pending_upload trước HTTP. Không reset hoặc ghi đè profile đã có để xử lý lỗi. Với account đã đăng ký nhưng mất IndexedDB, không tạo identity mới để thay khóa: báo mất khóa, dùng account demo mới nếu muốn làm lại; upload conflict 409 không thay IK/SPK trên server.

`Đăng ký public bundle` POST nguyên pending_upload. `Bổ sung 20 OPK` chỉ sinh batch mới khi không có pending upload; commit private keys và pending request trước POST. Nếu còn pending upload, nút gửi lại dùng đúng request, không sinh batch/ID/signature mới. Response 200 phải khớp user, IK/SPK, watermark không giảm và count hợp lệ; commit cập nhật registration/watermark rồi bỏ pending. HTTP/local save lỗi giữ pending đã commit để retry. OPK server đã claim không được insert lại khi retry upload.

Có pending_upload từ refill thì vẫn có thể nhận/decrypt bằng bộ khóa đã commit; chặn một refill mới và gửi E2EE mới tới khi upload được xác nhận. Nếu chưa xác nhận registration ban đầu thì chưa ready để tạo/gửi thread E2EE. Fingerprint hiển thị local IK và peer pin đã biết; Go trả SHA-256 hex raw IK32 qua bridge, không claim bundle để xem fingerprint. Lần đầu chưa có peer pin phải ghi chưa biết; TOFU không phải đối chiếu qua kênh tin cậy.

#### Gửi và retry

1. Yêu cầu tab sở hữu, runtime/state/registration ready, direct E2EE có mode và peer xác nhận từ summary. Có pending_send thì chặn message E2EE mới ở mọi thread; hiển thị thread/UUID chờ xác nhận và nút gửi lại/đối chiếu lịch sử/hủy pending chủ động.
2. Kiểm tra/trim text 1–1000 rune, UTF-8 hợp lệ, không NUL trước claim; sinh UUID một lần cho lần gửi logic. Giới hạn rune là Go kiểm tra cuối cùng, không chỉ maxlength UTF-16 của input.
3. Claim bundle bằng API II.4; verifyBundle qua WASM, kiểm tra user ID và so peer identity với pin đã có. Signature hoặc pin sai dừng; không encrypt bằng key chưa xác thực.
4. Gọi seal với context và DTO local. Go trả content string đã marshal một lần, message key và hash. JavaScript dựng outer POST body `{message_id, content_format:"e2ee_v1", content}` một lần.
5. Transaction local lưu key/context/hash/identity public, peer pin và pending_send gồm nguyên body trước POST. Save lỗi không POST; OPK public đã claim có thể bị phí, chấp nhận như II.4. Không lưu EK private.
6. POST bằng REST hiện có. Response 2xx chỉ xác nhận khi id, thread, sender, format và content đúng payload; commit bỏ pending trước khi coi gửi đã xác nhận. 503/network/timeout/5xx hoặc response sai giữ pending. 4xx cũng không tự tạo UUID/body mới; báo lỗi và giữ pending để đối chiếu/hủy.
7. Gửi lại sau lỗi hoặc reload chỉ POST nguyên body đã lưu, không claim lại, generate key/UUID/nonce hay seal lại. Nếu có automatic retry từ luồng hiện tại, nó cũng dùng đúng body durable này. Sửa ô text không thay payload đang pending; muốn gửi text mới phải giải quyết pending trước.
8. Hủy pending yêu cầu đúng UUID và hành động rõ của người dùng; commit bỏ pending nhưng giữ cached message key để đối chiếu nếu DB đã lưu. Hủy không chứng minh message chưa commit. REST history xác nhận đúng message có thể commit bỏ pending; payload khác không được coi là xác nhận.

#### Nhận event, REST và lưu khóa

1. Event WebSocket, response gửi, lịch sử ban đầu, trang cũ và REST catch-up đi qua cùng đường nhận. Chuẩn hóa REST id/WebSocket message_id về UUID; raw content vẫn là ciphertext opaque, không sửa message.content thành plaintext.
2. Gộp UUID và sắp seq bằng luồng hiện có. Cùng UUID nhưng seq/thread/sender/format/content khác phải báo conflict, không để MiniHermesRealtime.merge ghi đè raw payload/key. Giữ cursor dương/giảm và chống cursor lặp của fetchThroughBoundary. Không lấy toàn lịch sử thay pagination hiện tại.
3. Mode/peer chưa rõ thì giữ message pending trong RAM, fetch summary trước decrypt/send. Runtime/profile chưa ready hoặc tab khác đang sở hữu thì hiển thị placeholder trạng thái; không đánh dấu đã đọc E2EE. Khi tab sở hữu sẵn sàng, xử lý lại message đang chờ và REST hiện có; không thêm polling.
4. Context lấy từ outer response/event và thread summary: sender là mình hoặc peer, recipient tương ứng; recipient trong envelope phải khớp. Không dùng ID tùy ý từ envelope thay context. JavaScript có thể parse header để chọn local key; Go ParseEnvelope vẫn là validator bắt buộc.
5. Đã có message key: so context/identity public với cache và gọi decryptWithKey kèm expected_content_sha256. Hash/content/context khác thì dừng, không ghi đè key. Own message chưa có cached key báo thiếu khóa; không claim bundle hoặc cố tái tạo từ IK.
6. Received message mới: kiểm tra sender IK với pin nếu đã biết; chọn SPK/OPK private đúng ID; gọi open qua WASM. Thiếu OPK private khi header có ID là lỗi, không chuyển sang 3DH. Kiểm tra plaintext hợp lệ.
7. Sau open thành công, **cùng một transaction local** lưu message key/context/hash/identity public/pin và xóa OPK private đã dùng. Chỉ sau oncomplete mới đưa plaintext vào view RAM và render. AEAD hoặc save lỗi không xóa OPK, không giữ key mới như đã commit và không render plaintext.
8. Serialize event và REST cùng UUID; task sau đọc lại cache đã commit, dùng decryptWithKey thay open/consume lại. Duplicate event không tạo unread lần nữa hoặc bỏ qua việc render do crypto xong muộn; render lại khi decrypt hoàn tất dù raw merge báo changed=false.
9. Reload/reconnect giải mã lịch sử bằng cached key, không cần OPK private đã xóa. Không cache plaintext trong IndexedDB; mất state đồng nghĩa có thể mất khả năng đọc nhiều tin cũ.
10. Read marker chỉ tới seq thực tế có plaintext đã decrypt và render, trong chat active/tab visible/đang ở cuối viewport như web hiện có. Mọi tin E2EE đã tải trước mốc đó phải decrypt/cache thành công; không vượt message đang pending/error trong phần đã tải. Không cần tải toàn bộ trang cũ để đọc trang mới nhất, nhưng summary.last_seq, syncedSeq hoặc placeholder không chứng minh đã đọc. Web không tự PUT read E2EE khi thiếu ownership hoặc có lỗi.

### 9. Thao tác giao diện, realtime và gateway

| Trạng thái E2EE | Giao diện và điều kiện |
| --- | --- |
| initializing — Đang khởi tạo/khôi phục | Đang load runtime, lấy lock hoặc đọc profile; chặn gửi E2EE, giữ ciphertext chờ; plaintext/group hoạt động |
| ready — Sẵn sàng | Runtime + lock + profile + registration hợp lệ; cho gửi khi không có pending và mode/peer đã biết; decrypt event/REST |
| missing_keys — Thiếu khóa | Không có profile/key hoặc mất local state; không tự reset, không gửi/đánh dấu đọc E2EE; phân biệt account mới với account đã mất khóa qua thông báo/hành động khởi tạo rõ |
| other_tab — E2EE đang dùng ở tab khác | Không sở hữu lock; không đọc/sửa private state hay decrypt/send/refill/retry E2EE; có nút thử tiếp quản khi owner kết thúc |
| error — Lỗi E2EE | Runtime/storage/crypto/pin/upload lỗi; hiện nguyên nhân an toàn và thao tác retry phù hợp, không fallback plaintext hoặc sinh khóa mới |

Trạng thái nhận từng message ở RAM: đang giải mã, plaintext đã giải mã và commit, hoặc lỗi/thiếu khóa. Hiển thị lỗi trên message và banner account phù hợp; lỗi một message không làm mất khả năng xem plaintext/group, đọc metadata hoặc đối chiếu pending. Ready của account không biến message lỗi thành đã đọc.

Thao tác web cố định:

- Sau đăng nhập, khôi phục profile đã có; account mới có thao tác khởi tạo bộ khóa rõ theo II.8. Đăng ký public bundle, bổ sung 20 OPK hoặc gửi lại pending upload trên web; hiển thị watermark/count đã xác nhận, không coi count là last_prekey_id.
- Khi tạo direct mới, cho chọn plaintext hoặc E2EE, mặc định plaintext để tương thích. E2EE cần cả hai bundle; 409 nếu peer chưa đăng ký hoặc cặp đã có mode khác. Mở thread có sẵn dùng mode thực tế, không tạo thêm thread hoặc toggle mode cũ. Nhóm giữ plaintext.
- Gửi/nhận/giải mã ngay trong giao diện chat; E2EE chỉ bật input/button/submit ở tab ready sở hữu state. Fingerprint hiển thị UUID và SHA-256 hex IK đã biết để hai người đối chiếu ngoài kênh chat; không claim chỉ để xem.
- Banner pending hiển thị UUID/thread và nút Gửi lại, Đối chiếu lịch sử, Hủy pending; reload không mất payload. Không lưu draft plaintext của pending vào IndexedDB; có thể giải mã bản đã gửi từ cached key khi nó xuất hiện trong response/history.
- Render plaintext đã được kiểm tra bằng textContent, không innerHTML. Chưa decrypt hoặc không sở hữu state thì placeholder rõ trạng thái, không đưa nguyên envelope lên chat/sidebar/notification. Sidebar hiện chưa có preview; nếu thêm hoặc dùng last_message preview phải áp dụng cùng quy tắc, không decode riêng và không dựng lại payload.
- Không đổi canUseThread để mất quyền đọc hay thao tác nhóm; thêm điều kiện riêng cho E2EE send/read. Event tới trước summary vẫn nhận diện e2ee_v1 để giữ opaque/placeholder; fetch summary trước decrypt hoặc cho gửi vào thread chưa biết mode.
- Giữ reconnect backoff, heartbeat, ticket, REST cursor và nguyên tắc không polling. Lấy lịch sử ban đầu khi mở thread; catch-up sau socket mở lại thành công hoặc gap seq, giữ Tin cũ hơn. Async crypto/local save phải giữ snapshot phiên/thread/membership hiện có, không render kết quả cũ sang tài khoản khác.

Gateway tiếp tục chuyển nguyên ciphertext opaque; không sửa consumer group, cache membership, recipient snapshot, fan-out hoặc socket ticket cho tuần 5. Web có thể mở nhiều tab cho plaintext, nhưng E2EE chỉ một tab sở hữu scope; không xây đồng bộ khóa giữa tab/thiết bị.

### 10. Tổ chức file, phase và kiểm chứng

| Phần | Vị trí |
| --- | --- |
| Hợp đồng | `docs/e2ee-contract.md` |
| Nguồn và toàn bộ prompt | `Mini-Hermes-tuan5-Codex-prompts.md` hiện có, hoặc `docs/week5-codex-prompts.md` nếu được đặt tại đó |
| Wire types, crypto Go | `internal/e2ee/types.go`, `internal/e2ee/crypto.go` |
| Bridge Go/WASM | `cmd/e2ee-wasm/main_js.go`, constraint js && wasm; không có native client |
| Build artifacts | `web/e2ee.wasm`, `web/wasm_exec.js` sinh từ make wasm, ignore Git |
| Loader, state và controller web | `web/e2ee-wasm.js`, `web/e2ee-state.js`, `web/e2ee.js`; script thường cùng cách load hiện có, không thêm bundler |
| UI/realtime hiện có | Sửa `web/app.js`, `web/index.html`, style tối thiểu trong `web/style.css`; `web/realtime-core.js` chỉ khi cần guard UUID/payload, không viết crypto tại đây |
| Prekeys backend | `internal/module/e2ee/{model,dto,handler,service,repository}` |
| SQL public keys | `db/queries/prekeys.sql` |
| SQL thread/message | Queries hiện có trong `db/queries` |
| SQL generated | `internal/database/sqlc`, chỉ generate từ query |
| Wiring và asset routes | `cmd/main.go`, `internal/route/router.go`, Makefile, .gitignore |
| ADR | `docs/adr/004-e2ee-without-double-ratchet.md` |
| Demo và kết quả | `docs/e2ee-demo.md` |

Không tạo file rỗng cho đủ cây; có thể gộp DTO bằng alias wire types. Không thêm generic adapter/store, UI framework, service worker hoặc test suite web thường trực.

AGENTS.md hiện chỉ cho bốn file service test cũ. **Prompt 1** sẽ cập nhật đúng ngoại lệ cho **hai** file: `internal/e2ee/crypto_test.go`, `internal/module/e2ee/service/service_test.go`; giữ bốn file cũ và mọi quy tắc khác. Không có ngoại lệ test client riêng. Bridge/browser/IndexedDB/Web Locks/web/gateway dùng kiểm chứng thủ công hoặc script tạm rồi xóa theo AGENTS.md; không thêm test thường trực ở cmd hoặc web.

| Phase | Đầu vào | Đầu ra cố định |
| --- | --- | --- |
| Prompt 0 | Baseline hiện có, các quyết định web | Contract, nguồn/prompt đồng bộ, sơ đồ và bảng thay đổi; chưa implementation |
| Prompt 1 | Contract đã review | Crypto Go, native tests, bridge JSON, make wasm/runtime/assets; chưa HTTP E2EE hoặc tích hợp UI |
| Prompt 2 | Crypto/bridge đúng hợp đồng | API upload/claim và transaction prekeys; chưa tạo thread E2EE qua API |
| Prompt 3 | Prekeys API | Thread mode và gửi/lưu ciphertext opaque; web tạm chặn gửi sai mode/không read placeholder; chưa web decrypt |
| Prompt 4 | Backend hoàn chỉnh, bridge/build sẵn có | Web IndexedDB/Web Locks/send/decrypt/retry và demo A→B |
| Prompt 5 | Web A→B đã kiểm chứng hoặc giới hạn ghi rõ | Demo B→A, failure cases, regression và ADR #4; chỉ ghi PASS thực tế |

Crypto tests: 3DH/4DH cùng key, signature sai, tamper nonce/ciphertext/AAD/context/header, key low-order và envelope malformed. Service tests: validation/authorization/watermark, mode/format/retry/conflict/publish 503. Không mock mỗi primitive.

Script tạm bridge phải gọi đủ sáu method theo input/output JSON, xác nhận lỗi an toàn và parity với Go native. Script tạm web kiểm tra pending request byte-identical sau retry/reload, IndexedDB abort/quota không mất OPK/pending, same UUID event+REST, pin đổi, owner/other_tab, logout/scope, message decrypt trước read marker, WASM runtime lỗi và plaintext/group regression. Dùng browser có IndexedDB/Web Locks thật; mock hoặc Node không thay bằng chứng browser.

Transaction claim/upload/UUID/seq cần PostgreSQL thật với tài khoản/port/stream fixture riêng; không reset/TRUNCATE. make test, make build và make wasm là các check khác nhau. make sqlc khi SQL đổi; node --check cho JavaScript mới/đổi. Thiếu tool/service/browser thì ghi CHƯA CHẠY và cách tái hiện, không suy từ build/mock thành demo PASS.

### 11. ADR #4: quyết định và giới hạn bảo mật

ADR viết theo mạch: dữ liệu cần bảo vệ → X3DH giải quyết việc khởi tạo key → giao tiếp nhiều message cần giải quyết thêm việc gì → các lựa chọn → quyết định tuần này → điều kiện và đánh đổi.

Quyết định: lượt X3DH độc lập cho mỗi message; không duy trì SK cũ để trả lời; chưa có Double Ratchet; giữ message keys local để đọc lịch sử; identity/SPK cố định trong demo; cho phép 3DH khi hết OPK; TOFU và fingerprint để đối chiếu.

Phải phân biệt:

- **Confidentiality của nội dung:** API/DB/Redis không có private keys/message keys để giải mã trong thiết kế. Vẫn cần HTTPS/WSS khi đi qua mạng thực.
- **Forward secrecy:** bỏ Double Ratchet không có nghĩa mọi khả năng bảo vệ tin cũ của X3DH biến mất. OPK đã xóa và EK đã bỏ có thể bảo vệ secret của lượt cũ trước một số tình huống lộ khóa dài hạn về sau. Tuy nhiên, profile này giữ message keys local: ai lấy được toàn bộ state sẽ đọc được những message đã cache. Khi không có OPK và IK/SPK recipient của lượt đó bị lộ, các transcript cũ có thể bị giải mã; demo chưa xoay/xóa SPK để giảm khoảng ảnh hưởng.
- **Post-compromise recovery:** không có quy trình tự phục hồi bảo mật của một cuộc trò chuyện thông qua DH ratchet và thay/xóa trạng thái khóa. IK/SPK bị lấy vẫn cho phép các cuộc tấn công tiếp diễn; lượt X3DH mới mỗi tin không được mô tả là có đầy đủ PCS của Signal.
- **Replay:** UUID, content hash và AEAD context giúp nhận diện/ràng buộc message trong ứng dụng. Đây không phải trạng thái chống replay và ratchet đầy đủ của Signal; không dùng UUID hoặc hai nhãn HKDF theo chiều gửi để tuyên bố giải quyết mọi vấn đề reuse.
- **Authentication:** signature xác thực SPK với IK mà bundle cung cấp; chưa chứng minh IK đó thuộc đúng người khi mới gặp. TOFU có thể phát hiện thay đổi về sau, không loại MITM lần đầu. Fingerprint cần được hai người đối chiếu qua kênh tin cậy.

Metadata server vẫn thấy: tài khoản và cặp participant, thread/message UUID, seq, thời điểm, format, độ dài ciphertext, read marker/unread, public identity/prekeys/signature, prekey IDs, ephemeral public key, nonce và thông tin kết nối/IP nếu hạ tầng ghi lại. E2EE không che quan hệ ai nhắn với ai hoặc nhịp gửi tin.

Server không nên nhận/lưu plaintext của thread E2EE, identity/SPK/OPK private, EK private hoặc SK. Không ghi các giá trị này vào log. State client là ranh giới bảo mật riêng, chưa có mã hóa IndexedDB hoặc backup khóa trong phạm vi tuần này.

Giới hạn riêng của **client web Go/WASM**:

- IndexedDB/profile bị lấy sẽ lộ IK/SPK/OPK private, message keys và pending; message-key cache cho phép đọc lịch sử đã cache. Eviction/xóa storage, private browsing, mất profile hoặc chuyển origin có thể làm mất khóa; không có backup hay key recovery trong phạm vi.
- XSS, extension có quyền trên trang hoặc mã JavaScript/WASM/runtime bị thay có thể đọc keys từ IndexedDB/RAM, gọi bridge với keys, lấy plaintext hoặc gửi khóa ra ngoài. **WASM không tự bảo vệ khóa khỏi mã độc trong cùng trang**; Base64 không phải mã hóa storage. Không tuyên bố giữ Go crypto trong browser tạo ranh giới bí mật với JavaScript của cùng ứng dụng.
- Web Locks là điều phối tác vụ cùng origin/profile, không kiểm soát nhiều thiết bị, không chống mã độc và không xác minh danh tính peer. Serialize cùng local transaction giải quyết race/retry của demo, không bảo đảm chống mất điện hoặc khôi phục dữ liệu đã xóa.
- HTTPS, kiểm soát mã/asset và tránh XSS vẫn cần; artifact phải build cùng runtime compiler. Native test, WASM build hoặc source review không tự chứng minh browser/demo an toàn. Không ghi private keys/message keys/JWT/plaintext vào console, network debug hoặc tài liệu demo.

### 12. Nguồn để đối chiếu

- [X3DH specification](https://signal.org/docs/specifications/x3dh/): key agreement, publishing prekeys và các giới hạn replay/key reuse.
- [XEdDSA specification](https://signal.org/docs/specifications/xeddsa/): chữ ký với key format Curve25519; đối chiếu lưu ý biến thể thư viện ở trên.
- [Double Ratchet specification](https://signal.org/docs/specifications/doubleratchet/): ratchet và bảo mật sau khi khóa bị lộ.
- [go.mau.fi/libsignal v0.2.2 — ecc](https://pkg.go.dev/go.mau.fi/libsignal@v0.2.2/ecc): dependency chữ ký đã chốt; cần đọc source API, không import toàn bộ session/ratchet.
- [crypto/ecdh](https://pkg.go.dev/crypto/ecdh), [crypto/hkdf](https://pkg.go.dev/crypto/hkdf), [crypto/cipher](https://pkg.go.dev/crypto/cipher): primitive Go.
- [Go Wiki: WebAssembly](https://go.dev/wiki/WebAssembly): js/wasm build và wasm_exec.js cùng compiler; loader/runtime, không mặc định WASI.
- [syscall/js](https://pkg.go.dev/syscall/js): callback boundary của bridge Go/browser.
- [Web Locks API](https://developer.mozilla.org/en-US/docs/Web/API/Web_Locks_API): ownership giữa tab cùng origin và secure context.
- [Using IndexedDB](https://developer.mozilla.org/en-US/docs/Web/API/IndexedDB_API/Using_IndexedDB), [IDBTransaction](https://developer.mozilla.org/en-US/docs/Web/API/IDBTransaction): transaction local, completion/abort và lifetime.

## Đối chiếu baseline trước Prompt 1 — 04/10/2026

Branch feat/e2ee, HEAD `b5260e8fa902bd07c93763ab72022abc4adf52ff`, khớp baseline nguồn. Trước lượt này không có thay đổi tracked/staged; chỉ `?? Mini-Hermes-tuan5-Codex-prompts.md` và `?? docs/e2ee-contract.md` từ bước tài liệu trước. Không phát hiện thay đổi implementation khác baseline ảnh hưởng API/crypto; source Prompt 0 đã được sửa theo yêu cầu web mới.

| Hạng mục | Code hiện tại | Thiếu và file cần sửa/tạo ở bước triển khai |
| --- | --- | --- |
| Users/prekeys | Migration và sqlc model có identity_public_key, last_prekey_id và bảng prekeys signed/one_time/public/signature/unique namespace | Upload/claim/watermark/row-lock; internal/module/e2ee, db/queries/prekeys.sql, cmd/main.go và router |
| Thread mode | Schema plaintext/e2ee, group plaintext; CreateOrGetDirect khóa hai user ID tăng, cùng transaction thread/participants; query luôn insert plaintext, model/summary/DTO chưa mode | db/queries/threads.sql; module/thread/model, dto, handler, service, repository và caller/conversion/test liên quan |
| Message/retry/seq | Request chỉ message_id/content, service trim; Send và SQL hardcode plaintext. Thread lock/recheck membership/UUID trước tăng seq, unique toàn cục, conflict409; publish sau commit503 giữ định danh | db/queries/messages.sql; module/message/dto, handler, service, repository, lỗi domain/mock/caller/test; contentFormat argument trước content, opaque bytes E2EE |
| Event/gateway | Response/history/event/XADD/decode/fan-out đã mang content_format/content; recipient snapshot theo seq, sender nhận text own qua response/history | Giữ nguyên gateway/cache/ticket/consumer; không phải đổi ciphertext delivery |
| Web transport | app.js REST send, WS ticket/event, REST initial/older/catch-up; heartbeat/backoff không polling. merge core có cursor guard nhưng same UUID/same seq có thể overwrite payload | Giữ transport; guard conflict, queue async decrypt, view plaintext riêng và marker sau decrypt/save/render trong app.js/realtime-core.js |
| Web state/UI | JWT sessionStorage per tab, pending plaintext ở RAM; render trực tiếp content, read theo latest rendered; sidebar không có preview nội dung | web/e2ee-wasm.js, e2ee-state.js, e2ee.js, app.js, index.html và style tối thiểu; IndexedDB profile, Web Locks, UI states/fingerprint/pending |
| WASM/build/assets | Chưa internal/e2ee, cmd/e2ee-wasm, libsignal, make wasm hoặc WASM routes. Router chỉ serve index/app/realtime-core/style | Go crypto + bridge main_js.go, Makefile, .gitignore, router routes asset; libsignal v0.2.2 go.mod/go.sum; generated binary/runtime cùng Go |
| Test/tài liệu | AGENTS.md chỉ bốn service test cũ; chưa có ngoại lệ E2EE hay evidence browser | Prompt 1 sẽ thêm đúng hai ngoại lệ crypto/E2EE service; web/bridge script tạm. ADR #4 và e2ee-demo tạo ở phase sau |

**Schema PostgreSQL đủ cho thiết kế** theo migrations `20260924090000_create_direct_chat_schema.sql`, `20260928090000_use_external_message_id.sql` và models sqlc: public material, watermark/key namespace, mode/format, content TEXT, UUID unique và seq đã có. Không cần server session/ciphertext table hoặc migration mới. IndexedDB là state riêng ở browser; chưa xác minh migration/schema trên DB chạy thực tế. Generated sqlc chỉ cập nhật từ query khi triển khai.

## Bảng thay đổi thiết kế — 04/10/2026

| Điểm | Profile trước | Profile web đã chốt |
| --- | --- | --- |
| Client và transport | Client Go riêng, nhận lịch sử bằng thao tác thủ công | Web hiện có: REST send, WS receive, REST history/reconnect/gap |
| Crypto | internal/e2ee native | Cùng Go crypto, bridge syscall/js và Go/WASM; libsignal variant giữ nguyên |
| Bridge | Chưa có | Sáu method JSON string, local raw32 keys, context snake_case, {ok,data,error} cố định |
| Lưu bí mật | State local trong file | IndexedDB profile API URL + user UUID; không plaintext/JWT/EK private |
| Ownership | Một process sở hữu state | Web Lock một tab cùng origin/profile; Promise serialize mutations |
| Upload và pending | Private/payload lưu trước HTTP, retry exact | Giữ nguyên quy tắc; transaction IndexedDB oncomplete trước upload/POST/render |
| UI | Web chỉ placeholder/chặn E2EE | Init/restore/publish/refill/mode/fingerprint/send/decrypt/retry trên giao diện |
| Read và gộp tin | Client tự tải toàn bộ lịch sử | Giữ pagination/realtime, event+REST chung pipeline, read sau plaintext render |
| Build | Native test/build | Thêm make wasm, runtime cùng compiler; artifact không keys và không tracked |
| Test thường trực | Có ngoại lệ test client riêng dự kiến | Bỏ ngoại lệ client; chỉ hai file Go crypto/E2EE service ngoài bốn file cũ; web/script tạm |
| Phase | Một bước client ngoài web | Prompt1 Go+bridge/build, 2 prekeys, 3 backend opaque, 4 web A→B, 5 B→A/failure/ADR |
| Ranh giới bảo mật | State file có thể bị lấy | IndexedDB/XSS/JS-WASM bị thay có thể lộ khóa; WASM không cách ly khỏi mã cùng trang |

API prekeys/thread/message, Go function signatures, DH/KDF/AEAD, byte layout AAD/envelope, UUID retry/seq và một lượt X3DH mỗi tin **giữ nguyên**. Không thêm kiến trúc server, migration, outbox, ratchet hoặc crypto JavaScript.

Sơ đồ mới tại II.2 thể hiện browser + Go/WASM, private ở IndexedDB trước upload, PostgreSQL consume OPK trong transaction, encrypt trước REST POST, ciphertext qua WebSocket hoặc REST catch-up, recipient local commit/decrypt/render rồi mới read marker. Gateway không giữ keys/decrypt. Sáu bridge method tại II.7 và phase tại II.10 là đầu vào cố định cho Prompt 1–5 trong nguồn.

**Trạng thái tại bước tài liệu trước Prompt 1:** lượt đó chỉ sửa hai tài liệu. Prompt 1–5, native crypto mới, WASM build/runtime smoke, browser IndexedDB/Web Locks, demo A→B/B→A, integration/failure checks và ADR #4 đều **CHƯA TRIỂN KHAI / CHƯA CHẠY** tại thời điểm đó. Sơ đồ web mới chưa được người hướng dẫn review. Tiến độ sau đó nằm ở mục Prompt 1 bên dưới; không suy PASS browser/demo từ kết quả probe được nguồn ban đầu kể lại.

**Kiểm chứng tại bước tài liệu:** đối chiếu section II nguồn/contract, các phần API/crypto/AAD giữ nguyên, sáu bridge I/O, Prompt 0–5 và phase, JSON/code fences, không còn entry point/state/command/test client cũ; rà soát tĩnh sơ đồ và phạm vi file. Lượt đó chưa render Mermaid, chạy make test/build/wasm, browser hoặc migration; các check triển khai để lại đúng phase.

## Tiến độ Prompt 1 — 04/10/2026

**ĐÃ TRIỂN KHAI và kiểm chứng Prompt 1.** Branch vẫn `feat/e2ee`, HEAD vẫn `b5260e8fa902bd07c93763ab72022abc4adf52ff`; không checkout/reset/merge/push. Trước lượt triển khai không có thay đổi tracked/staged, chỉ hai tài liệu chưa tracked từ bước trước. Nguồn prompt không sửa trong lượt này; phần II contract không đổi. Các thay đổi hiện chưa commit.

### Đầu ra implementation

| File | Kết quả |
| --- | --- |
| `internal/e2ee/types.go` | Wire types public, KeyPair và MessageContext giữ đúng II.7; private DTO không dùng cho HTTP |
| `internal/e2ee/crypto.go` | Đủ chữ ký hàm II.7; X25519, DH ordering 3DH/4DH, FF32/zero32/info HKDF-SHA-256, AES-256-GCM, nonce mới mỗi Seal, byte layout AAD 199 byte; không dùng secret cũ cho chiều trả lời |
| `internal/e2ee/crypto_test.go` | Vector RFC 7748 §6.1 và RFC 5869 A.1/A.3; đối chiếu X3DH bằng DH/HMAC độc lập; golden AAD; A→B/B→A, 3DH/4DH, UTF-8 1–1000 rune, cache decrypt, signature/tamper/low-order/malformed/context/private pair và lời gọi sign/verify đồng thời |
| `cmd/e2ee-wasm/main_js.go` | Constraint js && wasm; globalThis.MiniHermesE2EE đúng sáu hàm và schema II.7, restore/fingerprint/hash; strict JSON tối đa 32 KiB, errors an toàn, callbacks sống; không HTTP/JWT/DOM/IndexedDB hay giữ state user |
| `Makefile` | make wasm cho Windows và Unix; Windows target dùng cmd.exe để tránh PowerShell mở rộng biến trước khi chạy recipe; build rồi copy runtime cùng compiler |
| `.gitignore` | Ignore riêng web/e2ee.wasm và web/wasm_exec.js |
| `internal/route/router.go` | Thêm GET /e2ee.wasm với application/wasm và asset /wasm_exec.js; cả hai no-store; chưa load runtime vào chat |
| `go.mod`, `go.sum` | Pin dependency chữ ký đúng v0.2.2 và dependency graph bắt buộc; Go version repo giữ nguyên |
| `AGENTS.md` | Thêm đúng hai ngoại lệ test được Prompt 1 cho phép, giữ bốn file cũ và các quy tắc khác; chưa tạo file test service E2EE của Prompt 2 |

Parser từ chối field thiếu/lạ/trùng/sai case, JSON thừa, Base64 không canonical padded, UUID không canonical/non-zero, ID không dương/ngoài int64, OPK ID trùng SPK, lengths sai và key low-order. Open/DecryptWithKey chỉ trả plaintext UTF-8 hợp lệ sau AEAD; khi lỗi không trả key/plaintext mới. Crypto/bridge không sửa hay xóa OPK/state local; việc chọn key đúng ID và transaction cache/xóa OPK còn thuộc Prompt 4. Sender retry vẫn phải giữ nguyên payload từ local state, không gọi Seal lại.

### Dependency và nguồn đã đọc

- `go.mau.fi/libsignal v0.2.2`, source tag tại commit `c231dd95d97ec136df1224fa5e4980a8bc39bb56`; LICENSE trong module ghi **GPL-3.0**. Chỉ import ecc; đã đọc Curve.go, DkbECPrivateKey.go, DjbECPublicKey.go, SignCurve25519.go và default logger. Không CreateKeyPair hoặc cấu hình/bật debug. Các lời gọi ecc được serialize vì logger mặc định lazy-init không có synchronization.
- Giữ ghi chú: chữ ký là **biến thể libsignal truyền sign bit**, không byte-for-byte XEdDSA canonical 2016, không tuyên bố audited hay tương thích Signal đầy đủ.
- `go mod tidy` bắt buộc thêm `filippo.io/edwards25519 v1.2.0`, nâng `x/crypto` 0.48.0→0.52.0, protobuf 1.36.10→1.36.11 theo libsignal; graph tiếp tục yêu cầu `x/net` 0.51.0→0.54.0, `x/sys` 0.41.0→0.45.0, `x/text` 0.34.0→0.37.0 và `x/sync` 0.19.0→0.20.0. Không tự chọn dependency/crypto khác hoặc nâng dependency ngoài graph này.
- Đọc lại [X3DH §4.2–4.3](https://signal.org/docs/specifications/x3dh/#43-replay-and-key-reuse): chiều B→A tạo lượt X3DH/EK/nonce mới với bundle A, không encrypt reply bằng SK của lượt A→B. Các giới hạn replay/không ratchet tại II.11 tiếp tục áp dụng.
- Vector test đối chiếu [RFC 7748 §6.1](https://www.rfc-editor.org/rfc/rfc7748.txt) và [RFC 5869 Appendix A](https://www.rfc-editor.org/rfc/rfc5869.txt). Đây là fixture test, không đưa private keys/message keys thực vào log hoặc tài liệu.

### Kiểm chứng đã chạy trên máy này

Go `go1.27.1 windows/amd64`, Node `v24.11.1`, Edge headless `154.0.4258.53`. Sandbox ban đầu chặn tải module, Make WinGet và một số entry Go cache; các lệnh được chạy lại với quyền truy cập cần thiết và đã hoàn tất. Target WASM ban đầu lỗi quoting, đã sửa và chạy lại; không lấy exit code của lần lỗi làm PASS.

| Lệnh / kiểm chứng | Môi trường | Kết quả thực tế |
| --- | --- | --- |
| gofmt trên files Go mới/đổi; go mod tidy | Go native | PASS; chỉ dependency graph bắt buộc |
| go test -v ./internal/e2ee | Go native | PASS các nhóm crypto; test đồng thời bổ sung đã được chạy trong make test cuối |
| make test | Go native | PASS, bao gồm crypto mới và bốn service test cũ |
| make build | Go native | PASS; js-only entry được loại theo constraint |
| make wasm | Go compiler js/wasm trên PowerShell | PASS, sinh web/e2ee.wasm và web/wasm_exec.js |
| Get-FileHash runtime build so với GOROOT/lib/wasm/wasm_exec.js | PowerShell | PASS, SHA-256 giống nhau; compiler cùng Go 1.27.1 |
| go vet ./... và GOOS=js GOARCH=wasm go vet ./cmd/e2ee-wasm | Go native / js/wasm | PASS |
| node --check script smoke tạm; node .tmp-e2ee-p1/node-smoke.cjs | Node | PASS đủ sáu hàm, 700 assertions; strict I/O, safe errors, restore/sign/verify/seal/open/cache hash/fingerprint |
| Trang smoke tạm, instantiateStreaming bằng runtime được build; Edge --headless=new --dump-dom | Browser Edge thật | PASS đủ sáu hàm, 700 assertions; DOM kết quả PASS, process exit 0; không suy ra từ Node |
| Native fixtures → WASM open/verify; output WASM → native Open/VerifyBundle bằng go run helper tạm | Go + Node, Go + browser riêng | PASS cả hai runtime, 4 trường hợp A→B/B→A × 3DH/4DH, cùng plaintext/message key/content hash/fingerprint và xác minh chữ ký |
| Script Go tạm dùng router thật, handler nghiệp vụ giả, httptest GET assets | Go native HTTP | PASS 6 asset routes mới/cũ, WASM magic, MIME và no-store; không dùng DB/Redis |
| git check-ignore; git diff --check; đối chiếu phần II nguồn/contract và phạm vi | Git / tài liệu | PASS; artifacts ignored, contract cố định giữ nguyên |
| go test -race ./internal/e2ee (bổ sung, ngoài yêu cầu Prompt 1) | Go native | CHƯA CHẠY được: CGO_ENABLED=0, máy không có gcc/clang trên PATH; không ghi race PASS |

Script/page/helper, fixtures và profiles browser smoke trong `.tmp-e2ee-p1` đã xóa sau chạy; server localhost smoke đã dừng. Hai artifact build được giữ ở web và ignore Git, không chứa keys/state người dùng. Không tạo test thường trực cho cmd/bridge/web.

### Đầu vào cho Prompt 2 và giới hạn còn lại

Prompt 2 sử dụng package e2ee, public wire types, ValidatePublicKey/VerifyBundle và bridge/build đã có theo II.7; error values của package là lỗi mô tả an toàn. Thực hiện API public prekeys và transaction/watermark/authorization theo II.4, không thay envelope/AAD/bridge hoặc crypto. Sau thay đổi Go crypto/bridge cần make wasm lại để cập nhật artifact cùng runtime.

**CHƯA TRIỂN KHAI:** upload/claim API, thread mode/message ciphertext, IndexedDB, Web Locks, loader/controller/UI chat, demo qua REST/WebSocket, ADR #4 và Prompt 2–5. Smoke browser chỉ kiểm chứng crypto bridge bằng fixture local trên router với handler giả; chưa chứng minh chat E2EE A→B/B→A qua API/PostgreSQL/Redis/gateway, pending durable/OPK local transaction hoặc read marker. Chưa kiểm chứng browser khác, UI tương tác, build Unix, race detector hoặc storage failure cases. Không sửa SQL, chạy migration hay reset database.

### Kiểm chứng lại Prompt 1 — 04/10/2026, khoảng 01:08 (+07:00)

Người dùng yêu cầu thực hiện lại Prompt 1 trên branch hiện tại. Đã đọc lại AGENTS.md, toàn bộ contract, Prompt 1 nguồn và code crypto/bridge/router/Makefile. Branch vẫn `feat/e2ee`, HEAD `b5260e8fa902bd07c93763ab72022abc4adf52ff`. Đầu lượt đã có các thay đổi chưa commit của Prompt 1 từ lượt trước; giữ nguyên chúng. Không phát hiện phần implementation thiếu hoặc khác contract cần sửa. So SHA-256 xác nhận tất cả files implementation, AGENTS.md, Makefile, .gitignore, go.mod/go.sum và nguồn prompt không đổi trong lượt kiểm chứng lại; chỉ bổ sung evidence này vào contract, build lại artifacts ignored.

| Lệnh / kiểm chứng trong lượt này | Môi trường | Kết quả thực tế |
| --- | --- | --- |
| gofmt -l trên 5 files Go mới/đổi | Go native | PASS, không có file cần format |
| go mod tidy | Go native | PASS, go.mod/go.sum không đổi |
| make test | Go native | PASS, kết quả test được Go cache |
| go test -count=1 ./internal/e2ee | Go native | PASS chạy mới, không dùng test cache |
| make build, make wasm | Go native / compiler js/wasm | PASS; runtime copy từ cùng compiler Go 1.27.1 |
| go vet ./...; GOOS=js GOARCH=wasm go vet ./cmd/e2ee-wasm | Go native / js/wasm | PASS |
| node --check script tạm; node .tmp-e2ee-p1-verify/node-smoke.cjs | Node v24.11.1 | PASS đủ sáu hàm, 655 assertions với fixture mới: exact schema, restore, fingerprint/hash, sign/verify, seal/open/cache decrypt, JSON/context/key/tamper errors an toàn |
| Trang smoke tạm dùng instantiateStreaming; Edge headless --dump-dom | Browser Edge 154.0.4258.53 thật | PASS đủ sáu hàm, 655 assertions; DOM ghi PASS và process exit 0 |
| go run ./.tmp-e2ee-p1-verify/native check node-result.json và browser-result.json | Go native, output Node/browser riêng | PASS mỗi runtime 4 trường hợp A→B/B→A × 3DH/4DH; xác minh chữ ký, plaintext, message key và hash. Native → WASM được kiểm tra trong smoke của từng runtime |
| Helper Go tạm, router thật với handler giả, httptest GET | Go native HTTP | PASS 6 routes asset mới/cũ, status/no-store và WASM MIME/magic; không dùng DB/Redis |
| So SHA-256 runtime với GOROOT/lib/wasm/wasm_exec.js; git check-ignore; đối chiếu phần II với nguồn | PowerShell / Git / tài liệu | PASS; đúng runtime cùng compiler, artifacts ignored, hợp đồng cố định giống nguồn |

Số 655 là của bộ smoke tạm mới trong lượt này; số 700 ở bảng trước là kết quả bộ smoke của lượt triển khai trước, không thay thế hoặc suy ra cho lượt này. Cả Node và browser đều đã chạy thực tế. Scripts/page/helper, fixture JSON và profile Edge trong `.tmp-e2ee-p1-verify` đã xóa sau chạy, server localhost tạm đã dừng. Không có test thường trực mới hoặc implementation ngoài Prompt 1.

**Đầu vào Prompt 2 vẫn như mục trên:** package crypto, wire types, bridge JSON và artifacts build đã sẵn sàng. Các giới hạn chưa kiểm chứng vẫn giữ nguyên: race detector (CGO/compiler C), build Unix/browser khác, chat UI, IndexedDB/Web Locks, API/DB/Redis/gateway và demo tích hợp. Chưa thực hiện Prompt 2–5, migration, đổi branch, push, merge hoặc reset DB.

## Tiến độ Prompt 2 — 04/10/2026 (lượt trước, DB chưa sẵn sàng)

**Implementation hai endpoint đã có; kiểm chứng PostgreSQL thật còn bị chặn.** Branch giữ `feat/e2ee`, HEAD giữ `b5260e8fa902bd07c93763ab72022abc4adf52ff`. Đầu lượt đã có thay đổi chưa commit của Prompt 1: `.gitignore`, AGENTS.md, Makefile, go.mod/go.sum, router và các file nguồn/contract/crypto/bridge mới. Đã đọc AGENTS.md, README, toàn bộ contract và evidence, Prompt 2 nguồn, crypto cùng code users/prekeys/threads/wiring; xác nhận trực tiếp package crypto, sáu hàm bridge, targets/assets và tests có trong working tree.

### Files và hành vi đã triển khai

| File mới/đổi trong Prompt 2 | Hành vi |
| --- | --- |
| `internal/module/e2ee/dto/prekeys.go` | Alias public UploadRequest/UploadResult/ClaimRequest/Bundle từ internal/e2ee; không DTO private |
| `internal/module/e2ee/model/errors.go` | Lỗi validation/conflict/mode/access/bundle; handler ánh xạ HTTP theo II.4 |
| `internal/module/e2ee/handler/handler.go` | Actor từ JWT context; JSON strict riêng hai endpoint, đúng tên/trường bắt buộc, từ chối trường lạ/trùng/null/sai kiểu/trailing data và UTF-8 sai; max body 16 KiB, kể cả Content-Length không biết; quá giới hạn 413; không log body/keys/raw DB errors |
| `internal/module/e2ee/service/service.go` | Validate UUID, Base64 canonical padded, key32/signature64/low-order/signature bằng crypto dùng chung, 0–100 OPK và int64 ID dương/không trùng; tra actor/recipient qua user service; không Gin |
| `internal/module/e2ee/repository/postgres.go` | Hai transaction bằng pgx.BeginFunc; upload khóa user và giữ IK/SPK bất biến; claim kiểm tra quyền rồi khóa recipient user, đọc bundle, DELETE RETURNING OPK; chỉ trả success khi commit thành công |
| `internal/module/e2ee/service/service_test.go` | Sáu nhóm service test trong đúng ngoại lệ AGENTS.md; validation, actor/context, exact upload retry/refill request, kết quả watermark/count do repo trả, self-claim, OPK null, lỗi quyền/repo và không lộ success khi lỗi |
| `db/queries/prekeys.sql` | Row locks users; reads/inserts public keys/watermark/count; khóa share thread/active participants giữ mode/membership ổn định; consume OPK nhỏ nhất bằng DELETE RETURNING |
| `internal/database/sqlc/prekeys.sql.go` | Sinh bằng make sqlc với sqlc v1.31.1; không viết tay |
| `internal/route/router.go` | E2EEHandler và hai POST routes trong JWT authenticated group; giữ các asset routes Prompt 1 |
| `cmd/main.go` | Wiring repository → service(userService) → handler → router; repository không JWT |
| `docs/e2ee-contract.md` | Trạng thái hiện tại, evidence thực tế, blocker và đầu vào bước sau; phần II cố định không sửa |

Upload ghi toàn bộ IK/SPK/OPK/watermark trong một transaction. IK và SPK ID/public/signature đã đăng ký phải khớp đúng byte, khác trả 409; retry không ký lại. Watermark là ID cao nhất đã cấp trong namespace chung SPK/OPK. Tất cả ID mới được so với watermark **đầu transaction**, nên batch không cần sắp ID; watermark cuối là max và không giảm. ID cũ còn row one_time phải có public key khớp, khác 409/rollback; ID cũ mất row hoặc nằm trong khoảng ID đã bỏ qua thì bỏ qua, không insert lại, không có tombstone để so public key lịch sử.

Claim chỉ cho hai user khác nhau là đúng hai participant active trong direct E2EE. Mode/kind không hợp lệ 409; self/ngoài thread/inactive hoặc cặp active không đúng 403; user/thread/bundle thiếu 404. Transaction giữ khóa share thread/active participant trong lúc kiểm tra quyền, rồi khóa recipient user cùng loại FOR UPDATE như upload. OPK nhỏ nhất được DELETE RETURNING, hết khóa trả field `one_time_prekey:null`; lỗi query/validation/commit không trả bundle thành công. Không Redis lock, SKIP LOCKED, reservation, idempotency claim hay hoàn trả OPK; mất response sau commit có thể hao một OPK đúng đánh đổi đã chốt.

Schema từ migrations/models hiện có đáp ứng public keys, unique `(user_id,key_id)`, watermark, mode và membership. **Chưa xác nhận schema của DB chạy thực tế** vì chưa kết nối được. Không thêm bảng, migration hoặc thay crypto/envelope/AAD/bridge/dependency/web/gateway. So SHA-256 14 files bảo vệ từ đầu lượt xác nhận AGENTS.md, Makefile, .gitignore, go.mod/go.sum, crypto/tests, bridge, app.js/realtime-core.js, nguồn prompt và hai artifacts build không đổi. Runtime build khớp SHA-256 của GOROOT/lib/wasm/wasm_exec.js cùng Go compiler.

### Kiểm chứng thực tế trong lượt Prompt 2

Go `go1.27.1 windows/amd64`, sqlc `v1.31.1`. Service tests chỉ dùng mock repository/user finder; HTTP script dùng router, JWT middleware, handler/service và crypto thật với repository mock, không PostgreSQL/Redis. Các giá trị fixture được tạo trong memory; script chỉ xuất số assertions và trạng thái an toàn. Mock không chứng minh watermark, rollback hoặc concurrency ở DB.

| Lệnh / kiểm chứng | Môi trường | Kết quả thực tế |
| --- | --- | --- |
| make sqlc | sqlc native | PASS sau khi sửa alias DELETE/subquery: lần đầu báo user_id ambiguous; generated query hiện tại đã sinh và compile |
| gofmt -w files Go mới/đổi | Go native | PASS |
| go test -count=1 -v ./internal/module/e2ee/service | Go native unit | Lần đầu phát hiện UUID fixture sai; đã sửa fixture, chạy lại không cache PASS |
| go test -count=1 ./internal/module/e2ee/service ./internal/e2ee | Go native unit/crypto | PASS chạy mới; sáu nhóm service, gồm 29 trường hợp upload invalid; crypto giữ nguyên |
| make test | Go native unit | PASS service E2EE mới, crypto và bốn service cũ; các test cũ được cache |
| make build | Go native | PASS |
| make wasm | Go compiler js/wasm | PASS; artifacts ignored, runtime từ cùng Go 1.27.1; bytes không đổi |
| go vet ./... | Go native | PASS |
| go run ./.tmp-e2ee-p2/http | Go native HTTP/httptest, repository mock | PASS 322 assertions: JWT/actor, exact request/response schema, 400/401/403/404/409/413/500/405, JSON strict/nested IDs/public keys/signature, body đúng/vượt 16 KiB với unknown Content-Length, OPK response/null, safe errors/logs và sáu asset routes/MIME/no-store. Lỗi assertion tái sử dụng map JSON trong script đã sửa trước lần PASS; không phải lỗi response API |
| go run ./.tmp-e2ee-p2/probe | pgxpool tới DB từ config/config.yml | FAIL kết nối: TCP connection refused, Windows 10061; exit 1. Không thực hiện query fixture/ghi dữ liệu/migration |
| docker ps | Docker CLI local | FAIL: không kết nối được named pipe daemon Docker Desktop; không chạy make up/migration |
| So SHA-256 files đã có/runtime; git diff --check; kiểm tra phần II và danh sách test | PowerShell/Git/tài liệu | PASS: phần II giữ đúng nguồn, 14 files bảo vệ giữ nguyên, chỉ sáu file test được phép |

Scripts/helpers trong `.tmp-e2ee-p2` đã xóa sau kiểm chứng theo AGENTS.md; không tạo test HTTP/repository/web thường trực. Không chạy Node/browser hoặc demo chat trong lượt này. Evidence Node/Edge của Prompt 1 là lịch sử riêng, không dùng làm PASS cho API/prekeys/transaction.

### Blocker DB và phần chưa kiểm chứng

PostgreSQL theo cấu hình hiện tại từ chối TCP; Docker daemon cũng chưa sẵn sàng. Cần bật PostgreSQL local có schema hiện có hoặc cung cấp PostgreSQL thử nghiệm truy cập được. Đã yêu cầu thông tin môi trường trong lượt này; chưa có DB khả dụng để hoàn tất kiểm chứng tích hợp. Không reset/TRUNCATE, sửa mode thread sẵn có hay chạy migration để vượt blocker.

**CHƯA CHẠY trên PostgreSQL thật:** upload lần đầu/0–100 OPK, retry và refill với batch ID không sắp; bất biến IK/SPK/signature; old ID khớp/không khớp/mất row; rollback toàn batch khi conflict; quyền direct E2EE/actor/peer active và thiếu user/thread/bundle; claim có OPK/OPK nhỏ nhất/hết OPK; claim đồng thời không trùng; upload và claim đồng thời; retry upload sau consume không làm sống lại OPK; query/commit failure không trả 200 và rollback. Service/HTTP mock PASS ở bảng trên không thay các mục này. Do đó chưa đánh dấu Prompt 2 hoàn tất kiểm chứng DB.

### Đầu vào cụ thể cho Prompt 3

1. Hai endpoint public prekeys và JWT wiring đã có đúng II.4; DTO dùng chung `internal/e2ee`. Service `Upload(ctx, actorExternalID, UploadRequest)` / `Claim(ctx, actorExternalID, recipientExternalID, ClaimRequest)` tra internal ID qua userService. Repository `Upload(ctx, userID, UploadRequest)` / `Claim(ctx, actorID, recipientID, threadExternalID)` chứa transaction/authorization/watermark/consume.
2. Router.New hiện nhận thêm E2EEHandler sau WSTicketHandler và trước middleware authenticate; cmd/main.go đã truyền handler mới. Giữ routes/artifacts/crypto/bridge của Prompt 1. SQL mới cần make sqlc khi đổi query; không đổi migration.
3. Prompt 3 phải đọc blocker này và hoàn tất DB checks còn thiếu khi DB sẵn sàng; không suy ra integration PASS từ unit/HTTP mock. Chưa có API tạo direct E2EE ở code hiện tại: direct query vẫn insert plaintext; thread model/summary/DTO chưa trả encryption_mode. Không đổi mode thread cũ hay tạo cặp thứ hai để demo claim.
4. Message DTO/service/repository vẫn chỉ gửi plaintext, chưa lưu e2ee_v1 opaque/mode/format/header theo contract. Prompt 3 tiếp tục đúng các file thread/message/query/caller/tests đã liệt kê trong baseline và Prompt 3 nguồn; giữ UUID retry/seq/503 publish và gateway ciphertext opaque.
5. Chưa triển khai IndexedDB, Web Locks, web loader/controller/decrypt/read marker, chat A→B/B→A hoặc ADR #4; thuộc Prompt 4–5. Không tự bắt đầu Prompt 3 trong lượt này, đổi branch, push, merge hoặc reset DB.

## Hoàn tất kiểm chứng Prompt 2 — 04/10/2026

**Prompt 2 đã hoàn tất trong phạm vi hai endpoint backend. Blocker PostgreSQL ở mục lịch sử phía trên đã được gỡ.** Branch vẫn `feat/e2ee`, HEAD vẫn `b5260e8fa902bd07c93763ab72022abc4adf52ff`. Lượt này đọc lại AGENTS.md, README, contract/evidence/đầu vào các phase, Prompt 2 nguồn và code users/prekeys/threads/wiring. Kiểm tra trực tiếp crypto/wire types/tests, bridge đủ sáu method với constraint js && wasm, targets/runtime/routes asset của Prompt 1 và cả hai endpoint Prompt 2 trong working tree. Implementation đã có đúng hợp đồng; sử dụng nguyên code đó để kiểm chứng, không suy trạng thái từ HEAD.

Đã khởi động Docker Desktop đang cài trên máy rồi chạy `make up`, bật các container PostgreSQL/Redis hiện có với volume giữ nguyên. PostgreSQL **16.15** truy cập được từ `config/config.yml`; đọc schema xác nhận users/prekeys/threads/participants và mode/keys/watermark/membership đáp ứng queries hiện tại. Threads đã dùng schema sau bỏ direct pair columns. **Không cần và không chạy migration; không thêm bảng/schema/trigger hoặc reset/TRUNCATE/reseed sequence.** Không sửa config hoặc tiết lộ URL/JWT secret.

### Kiểm chứng HTTP với PostgreSQL thật

Helper Go tạm dùng router, JWT middleware, E2EE handler/service/repository, user service/repository và queries sqlc thật, kết nối PostgreSQL qua pgxpool. HTTP requests đi qua `httptest`; các handler ngoài E2EE được stub và không nằm trong kết quả kiểm chứng. Không mock repository/users/transaction của hai endpoint.

Fixture là sáu tài khoản có username/UUID ngẫu nhiên và tám thread mới trong các bảng sẵn có. Direct E2EE fixture được **INSERT với mode ban đầu e2ee** để kiểm tra claim; không đổi mode thread cũ, không triển khai API tạo E2EE của Prompt 3, không gọi send message và không coi đây là demo chat. Private keys fixture chỉ ở memory của phần tạo request, HTTP upload/claim chỉ chứa public DTO. JWT dùng realm ngẫu nhiên riêng helper; output chỉ có trạng thái/số assertions, không keys/JWT/body/DB errors thô.

| Nhóm | Bằng chứng PostgreSQL/HTTP thực tế |
| --- | --- |
| Upload lần đầu và retry | Đăng ký 0 OPK; batch ID không sắp 9/2; retry nguyên payload; response user/IK/SPK/watermark/count so với DB sau commit; một SPK giữ nguyên |
| Refill và watermark | Refill 11/10 cùng request, so floor đầu transaction; watermark max chứ không count; 100 OPK trong request với SPK ID 101 lớn hơn mọi OPK; exact retry; int64 max cho SPK |
| ID cũ và chống hồi sinh | Claim ID nhỏ nhất 2; retry payload cũ giữ count giảm; public khác của ID đã mất row được bỏ qua; old ID nằm trong khoảng trống được bỏ qua; old ID 9 còn row/public khác trả 409 và rollback insert ID 12/watermark của cả batch |
| Bất biến public bundle | IK khác với signature hợp lệ, SPK ID/public khác, hoặc signature hợp lệ mới cho cùng IK/SPK đều 409; DB state không đổi |
| Input/JWT/body | Trường chọn owner/private key, JSON thiếu/lạ/trùng/sai case/trailing/null, ID trùng SPK/0/âm/float/ngoài int64, Base64/signature/low-order sai và 101 OPK bị từ chối; 401 JWT thiếu/sai, 404 actor không tồn tại; body đúng 16 KiB được nhận, vượt 16 KiB trả 413 cả unknown Content-Length; invalid input không đổi DB state |
| Claim quyền | Self/actor ngoài thread/recipient ngoài thread/inactive actor hoặc peer/cặp active thiếu hoặc dư đều 403; plaintext/group 409; thiếu recipient/thread/bundle 404; denied claim không consume; chiều B claim A dùng đúng vai trò |
| Consume và hết OPK | Các lượt claim lấy 2 rồi 9/10/11 theo thứ tự; bundle qua VerifyBundle; chỉ bốn field public và one_time_prekey:null khi hết; retry initial upload sau consume hết giữ count 0 |
| Concurrency | Ba đợt **40 claim đồng thời**, dùng nhiều connection với pool max 16: 20 OPK cấp sẵn; xen bốn upload retry; xen bốn upload refill mới. Không ID nào lặp giữa response đã commit; tổng còn lại + OPK đã nhận đúng 20; watermark không giảm; retry không hồi sinh |
| Row lock | Một transaction kiểm soát giữ FOR UPDATE recipient; pg_stat_activity ghi claim chờ LockE2EEUser, chưa có response; nhả lock thì claim mới consume và trả bundle |
| Commit trước response | QueryTracer của helper chặn ngay trước COMMIT: response chưa trả, DELETE chưa visible từ connection khác; thả barrier rồi response 200 và OPK đã biến mất |
| Lỗi và rollback | Inject context cancel tại COMMIT upload/claim và DELETE query trên pool test: 500, không bundle thành công, DB keys/watermark/OPK được rollback. Một OPK fixture có public low-order lưu thủ công cũng 500 và DELETE staged được rollback; sửa lại đúng public fixture rồi vẫn claim được key đó |
| Cleanup | Xóa chỉ thread/user với cặp internal ID + UUID đã tạo; FK cascade dọn participants/prekeys. Counts ở users/prekeys/threads/participants/messages trở về baseline trước mỗi lần chạy; query sau helper thấy 0 fixture username và 0 connection application_name của helper. Identity sequences tăng tự nhiên theo INSERT fixture, không reset |

### Lệnh và kết quả của lượt hoàn tất

Go `go1.27.1 windows/amd64`, sqlc `v1.31.1`, PostgreSQL `16.15` trong container local. Hai lần chạy helper DB đều PASS và tự dọn fixture; lần sau thêm lỗi DELETE/stored invalid OPK và dùng kết quả đầy đủ nhất dưới đây. Số assertions thay đổi theo số claim đến trước refill và số lần quan sát lock wait, không phải số case cố định.

| Lệnh / kiểm chứng | Môi trường | Kết quả thực tế |
| --- | --- | --- |
| Start-Process Docker Desktop; make up | Windows/Docker Compose local | PASS; PostgreSQL/Redis đang running, volume cũ giữ nguyên |
| docker compose exec -T postgres psql ... SELECT version()/schema | PostgreSQL thật, chỉ đọc | PASS; version/schema hiện có phù hợp, không migration |
| go run ./.tmp-e2ee-p2-verify/probe | Go native → DB trong config | PASS kết nối và bốn bảng cần dùng |
| go run ./.tmp-e2ee-p2-verify/integration | Go native HTTP/httptest + PostgreSQL thật | PASS **1472 assertions** ở lần chạy đầy đủ cuối; gồm tất cả nhóm ở bảng trên. Lần trước 1481 assertions với ít failure cases hơn và lịch interleaving khác; không dùng số đó thay kết quả cuối |
| make sqlc | sqlc native | PASS; generated code giữ nguyên byte |
| make test | Go native unit | PASS; service E2EE chạy, các nhóm cũ/crypto được cache |
| go test -count=1 ./internal/e2ee ./internal/module/e2ee/service | Go native unit/crypto | PASS chạy mới, không test cache; unit vẫn tách riêng với DB helper |
| make build | Go native | PASS |
| make wasm | Go compiler js/wasm | PASS; artifacts/runtime cùng Go 1.27.1, giữ nguyên bytes |
| go vet ./...; gofmt -l files Go liên quan | Go native | PASS, không file cần format |
| So SHA-256 87 files đã có, gồm implementation/source/migrations/web/dependencies/artifacts | PowerShell | PASS, không file nào đổi; contract được loại khỏi snapshot vì bổ sung evidence |
| Runtime SHA-256 so với GOROOT/lib/wasm/wasm_exec.js; fixed contract phần II so nguồn; git diff --check; danh sách *_test.go | PowerShell/Git/tài liệu | PASS; runtime đúng compiler, hợp đồng giữ nguyên, chỉ sáu test files được phép |

**File thay đổi trong lượt hoàn tất này chỉ là `docs/e2ee-contract.md`.** Các file implementation Prompt 2 đã tạo ở lượt trước được giữ nguyên và đã kiểm chứng trên DB thật: `internal/module/e2ee/{dto/prekeys.go,model/errors.go,handler/handler.go,service/service.go,service/service_test.go,repository/postgres.go}`, `db/queries/prekeys.sql`, generated `internal/database/sqlc/prekeys.sql.go`, wiring `cmd/main.go` và `internal/route/router.go`. Thay đổi Prompt 1/nguồn/tài liệu khác vẫn được giữ, không stage/commit/push/merge/checkout/reset.

Helpers trong `.tmp-e2ee-p2-verify` đã xóa sau kiểm chứng; không thêm test thường trực HTTP/repository/web. Container local giữ running để dùng ở bước sau. **Không chạy Node/browser/chat demo/Redis event/gateway trong lượt này.** Kết quả này xác nhận hai endpoint với PostgreSQL thực qua httptest, chưa xác nhận thao tác web E2EE hoặc đường tạo thread/gửi ciphertext của Prompt 3–5. Context-cancel failure checks kiểm tra lỗi client DB tại query/commit; chưa mô phỏng server crash hoặc mất response trên mạng sau commit. Giới hạn browser khác/build Unix/race detector của Prompt 1 vẫn giữ nguyên.

### Đầu vào Prompt 3 sau khi hoàn tất Prompt 2

1. **Không còn blocker DB của lượt trước.** Config hiện tại kết nối được PostgreSQL; schema sẵn có đã dùng thành công cho tests. Hai endpoint JWT upload/claim, transaction/watermark/authorization/consume và tests service đã có; evidence DB ở trên thay thế trạng thái CHƯA CHẠY của lượt trước. Không cần triển khai lại prekeys hoặc sửa API/crypto để tiếp tục.
2. Public DTO vẫn alias `internal/e2ee`. Service và repository signatures, SQL query names cùng vị trí file ở đầu vào Prompt 3 của mục lịch sử giữ nguyên. Router.New nhận E2EEHandler sau WSTicketHandler và trước authenticate; cmd/main.go đã wired. Giữ sáu method bridge, crypto/envelope/AAD, runtime/build/assets/dependencies và chỉ generate sqlc từ SQL khi cần.
3. **Còn thuộc Prompt 3:** API tạo/mở direct theo encryption_mode và trả mode ở summary/DTO/model; query direct hiện vẫn insert plaintext. Khi tạo direct E2EE mới, kiểm tra registered IK/SPK của cả hai trong transaction khóa hai user theo ID; cặp cũ mode khác trả 409, không đổi mode/lịch sử/cặp. Fixtures vừa kiểm tra đã xóa, không có đường API tạo E2EE được bổ sung ở Prompt 2.
4. Message request/service/repository/query vẫn là plaintext; cần đường lưu e2ee_v1 opaque, body/header/format/mode validation, cập nhật Send contentFormat trước content và mọi callers/mocks; giữ UUID retry, seq/transaction và lỗi publish 503. Event/gateway hiện chuyển format/content và giữ nguyên transport/snapshot/ticket.
5. Chưa có IndexedDB/Web Locks/loader/controller/chat decrypt/read marker hoặc demo A→B/B→A, ADR #4; vẫn thuộc Prompt 4–5. Lượt này dừng sau Prompt 2, không bắt đầu Prompt 3.

## Hoàn tất Prompt 3 — 04/10/2026

**Prompt 3 đã hoàn tất trong phạm vi backend và guard web tạm. Không còn blocker của bước này.** Branch giữ `feat/e2ee`, HEAD giữ `b5260e8fa902bd07c93763ab72022abc4adf52ff`; không checkout/reset/merge/stage/commit/push. Đã đọc AGENTS.md, README, contract gồm evidence/đầu vào, Prompt 3 nguồn và code thread/message/event/gateway/web. Đối chiếu trực tiếp đầu ra Prompt 1–2 trong working tree: crypto/wire types/tests, bridge sáu hàm, WASM/build/assets, upload/claim và wiring đều đã có. Giữ các thay đổi chưa commit của hai bước trước; không suy trạng thái từ HEAD.

### Files thay đổi trong Prompt 3

| Files | Chức năng đã triển khai |
| --- | --- |
| `db/queries/threads.sql`, `internal/database/sqlc/threads.sql.go` | Mode trong create/find/summary/list; giữ khóa hai user theo ID tăng. Generated bằng make sqlc |
| `internal/module/thread/model/thread.go`, `model/errors.go`, `dto/thread.go`, `handler/handler.go`, `service/service.go`, `repository/postgres.go` | Request `EncryptionMode *string`, validate mode, response actual mode; bundle hợp lệ của cả hai khi tạo E2EE mới; conflict khi mode cặp đã có khác yêu cầu |
| `internal/module/thread/service/service_test.go` | Cập nhật interface/mock/callers; kiểm tra omitted/explicit/invalid mode và giữ lỗi conflict/thiếu bundle |
| `db/queries/messages.sql`, `internal/database/sqlc/messages.sql.go` | Lock thread trả ID/kind/mode; query participant/public IK; insert nhận content_format. Generated bằng make sqlc |
| `internal/module/message/dto/message.go`, `model/errors.go`, `handler/handler.go`, `service/service.go`, `repository/postgres.go` | Truyền contentFormat trước content; body/format/envelope/header/mode validation; ciphertext nguyên byte; UUID retry trước mode/header của message mới trong transaction |
| `internal/module/message/service/service_test.go` | Cập nhật mock/callers; legacy plaintext, envelope invalid/oversize, opaque response/event, format/header/access errors và retry sau publish lỗi |
| `web/app.js` | Guard tạm: placeholder theo e2ee_v1 hoặc mode e2ee; chặn input/button/submit/REST plaintext retry và read marker cho ciphertext/chưa biết mode; event trước summary lấy lại summary bằng REST |
| `docs/e2ee-contract.md` | Evidence thực tế và đầu vào Prompt 4; phần II cố định giữ nguyên văn nguồn |

Tổng cộng **19 files** thay đổi trong bước này, gồm hai files generated và contract. Không sửa package crypto/bridge, prekeys API của Prompt 2, dependencies, Makefile, router/wiring, gateway, membership cache, WebSocket ticket, realtime-core, migration hoặc schema. Không thêm test thường trực ngoài hai service test hiện có. SHA-256 của **27 files bảo vệ** chụp đầu lượt — AGENTS/Makefile/.gitignore/dependencies/nguồn prompt, crypto/bridge, module prekeys, gateway/membership, migrations, realtime-core và artifacts — giữ nguyên sau kiểm chứng/build.

### Hành vi backend và giới hạn guard

- Direct mới omitted mode mặc định plaintext; explicit chỉ plaintext/e2ee, null/empty/sai kiểu không được coi là omitted. Direct đã có omitted trả mode thực tế; explicit khác mode trả 409, không tạo cặp thứ hai hoặc đổi mode. Tạo E2EE mới khóa hai user theo ID tăng và VerifyBundle IK/SPK của cả hai ngay trong transaction; thiếu hoặc public bundle không hợp lệ trả 409. Group giữ plaintext và nghiệp vụ hiện có.
- Send handler giới hạn outer body 16 KiB, kể cả unknown Content-Length; quá giới hạn 413. Omitted/empty format là plaintext; unknown format 400, format hợp lệ nhưng không khớp mode 409. Plaintext giữ trim/UTF-8/1–1000 rune/no NUL. E2EE dùng ParseEnvelope của package chung, giới hạn 8 KiB, không trim/normalize/marshal lại content.
- Transaction gửi giữ thread lock và kiểm tra membership active, rồi so UUID/sender/thread/kind/format/content với row cũ trước mode/header của message mới. Retry đúng trả row/seq cũ; conflict 409 chung. Message mới kiểm tra recipient là peer active, sender IK khớp actor và recipient SPK ID khớp SPK đã đăng ký; không query/yêu cầu OPK còn trong DB. Seq/insert cùng transaction, publish sau commit và 503/retry/snapshot giữ hành vi cũ; không outbox.
- REST response/history, Redis và gateway giữ nguyên content_format/content. Server chỉ xác nhận cấu trúc và public header, **không xác nhận AEAD hoặc plaintext có thể giải mã**. Fixture header hợp lệ vẫn được server nhận khi message UUID/context đã đổi; đây là kiểm chứng ciphertext opaque, không phải crypto thành công của context đó.
- Web vẫn REST send, WebSocket receive, REST history/older/reconnect/gap catch-up, không polling. `canUseThread` giữ nguyên quyền history/group; `canSendPlaintext` là guard riêng. Placeholder không chứng minh đã đọc: read sequence bằng 0 và không PUT read khi mode e2ee, có message e2ee_v1 hoặc mode chưa rõ. Event nhận diện format trước summary; raw content giữ trong cache để Prompt 4 giải mã. Chưa có thao tác tạo mode E2EE, upload/refill hoặc decrypt trên giao diện.

### Kiểm chứng thực tế

Go `go1.27.1 windows/amd64`, sqlc `v1.31.1`, Node `v24.11.1`, PostgreSQL `16.15`, Redis container local. Đã khởi động Docker Desktop và chạy make up với các volume hiện có. Không sửa config, tạo bảng, migration/reset/TRUNCATE/reseed sequence.

| Lệnh / kiểm chứng | Môi trường | Kết quả thực tế |
| --- | --- | --- |
| make sqlc | sqlc native | PASS, generate hai query files thread/message; chỉ sinh từ SQL |
| gofmt -w; gofmt -l trên 13 files Go đổi của thread/message | Go native | PASS, files đổi được format |
| go test -count=1 ./internal/module/thread/service ./internal/module/message/service | Go native unit, repository/publisher mock | PASS chạy mới; không dùng kết quả này làm bằng chứng transaction DB |
| make test | Go native unit/crypto | PASS cả sáu service/crypto test files; một số nhóm cũ được Go cache |
| make build; make wasm | Go native / compiler js/wasm | PASS; runtime từ cùng Go 1.27.1; artifact bytes giữ nguyên |
| go vet ./...; node --check web/app.js | Go native / Node syntax | PASS |
| go run ./.tmp-e2ee-p3/db | Go native HTTP/httptest + PostgreSQL/Redis thật + gateway WebSocket thật | PASS **563 assertions** ở lần chạy đầy đủ cuối; không mock repository/transaction/publisher/consumer/hub của luồng đã kiểm tra |
| node --check .tmp-e2ee-p3/web-guard.cjs; node .tmp-e2ee-p3/web-guard.cjs | Node VM, DOM/fetch giả | PASS **51 assertions** cho guard; không browser/decrypt demo |
| docker compose exec -T postgres psql ... fixture/connection counts | PostgreSQL thật, chỉ đọc | PASS, 0 tài khoản p3_ và 0 connection application_name mini-hermes-p3-fixture sau helper |
| SHA-256 27 files bảo vệ/runtime; phần II so nguồn; git diff --check; danh sách *_test.go | PowerShell/Git/tài liệu | PASS: giữ implementation các bước trước, runtime đúng compiler, phần II giống nguồn, chỉ sáu test files cho phép |

Helper DB dùng sáu tài khoản mới với UUID/username ngẫu nhiên, HTTP router/JWT/handlers/services/repositories thật và stream/cache Redis có namespace ngẫu nhiên riêng. **Thread E2EE được tạo qua API**, sau upload public bundle của hai user, rồi claim OPK và POST envelope; không INSERT mode E2EE thay cho API. Crypto Seal/Open chỉ ở phần client fixture Go native trong memory, không trong API. Private keys/message key chỉ ở memory client fixture; JWT dùng cho HTTP authentication. Không ghi các giá trị này vào DB/log/output; upload/claim chỉ public DTO. REST dùng httptest, gateway dùng socket mạng localhost thật; không phải browser demo.

| Nhóm DB/HTTP thực tế | Bằng chứng |
| --- | --- |
| Thread mode/bundle | Omitted/explicit tạo mới và lấy lại; mismatch 409; invalid/null/empty/sai kiểu 400; missing bundle ở actor hoặc peer và fixture signature sai 409; group response plaintext; list/summary trả actual mode |
| Chống tạo trùng | 32 requests đồng thời với hai chiều actor/peer; cùng một UUID/thread, DB chỉ một row cho cặp |
| Envelope/format/header/quyền | Unknown format 400, plaintext/E2EE/group mismatch 409; malformed/extra/trailing/version/field/body-size errors; recipient self/ngoài thread, sender IK và SPK ID sai 400; thiếu JWT 401, ngoài thread gửi/lịch sử 403; rejected request không tăng seq |
| Bytes/OPK | OPK đã consume trước POST vẫn gửi được. Content có indentation và whitespace vượt 1000 rune giữ nguyên trong PostgreSQL, HTTP response/history, Redis stream và frame gateway; giới hạn content đúng 8 KiB nhận/vượt một byte 400; outer body đúng 16 KiB nhận/vượt một byte 413 cả unknown Content-Length |
| UUID/retry/atomic seq | Cùng UUID/payload trả row/seq cũ; khác content bytes/format/sender/thread 409. Inject SPK ID sai tạm trên fixture rồi retry vẫn trả row cũ, chứng minh không chạy header của message mới. 32 sends cùng UUID: đúng một 201, các retry 200/cùng seq và một DB row. 12 message UUID mới đồng thời có seq liên tục/không trùng và last_seq đúng |
| Publish 503/retry | Đặt sai kiểu Redis tại key stream fixture để XADD thật lỗi sau commit: HTTP 503 mang UUID/seq. Xóa riêng key lỗi rồi retry đúng payload trả 200/cùng row/seq, XADD thành công; DB chỉ một message |
| Plaintext/group/history | Client cũ omitted/empty format, trim, retry; group create/system/text, cursor before_seq, read marker, leave và giới hạn lịch sử của cựu thành viên giữ hoạt động; gửi sau leave 403 |
| Cleanup | Chỉ xóa thread/user của fixture, cascade dọn participants/messages/prekeys và xóa keys trong namespace Redis riêng; counts năm bảng về baseline, không reset sequence. Consumer/socket/server helper đã dừng |

Script Node chạy nguyên app.js/realtime-core.js hiện có trong VM: event trước summary và REST history đều placeholder; composer/submit/direct retry bị chặn; không PUT read ciphertext hoặc mode chưa rõ; plaintext direct/group vẫn send/read; failure rồi xuất hiện bằng chứng E2EE không phát POST plaintext lần hai. **DOM/fetch giả không chứng minh browser behavior.** Scripts/helpers trong `.tmp-e2ee-p3` đã xóa sau kiểm chứng theo AGENTS.md; không thêm test HTTP/repository/web/gateway thường trực. PostgreSQL/Redis giữ running cho bước sau.

**CHƯA KIỂM CHỨNG trong lượt này:** browser UI, chat E2EE A→B/B→A qua Go/WASM, IndexedDB/Web Locks/pending durable/xóa OPK local/read sau decrypt, storage/runtime failures hoặc nhiều tab. Chưa chạy lại bridge smoke Node/browser của Prompt 1; WASM build chỉ là kiểm tra compiler, evidence bridge lịch sử vẫn tách riêng. Chưa mô phỏng server crash hoặc mất response mạng sau commit; chưa chạy race detector/build Unix/browser khác. Không ghi các mục này PASS và không triển khai Prompt 4–5.

### Đầu vào cụ thể cho Prompt 4

1. Backend upload/claim/direct E2EE/message opaque đã có và được kiểm chứng như trên. API và wire types giữ nguyên II.4–6. Account pair đã có plaintext không đổi mode: demo dùng cặp account mới đã đăng ký IK/SPK. Claims consume OPK ngay sau commit, response bị mất vẫn có thể hao OPK như contract.
2. `CreateOrGetDirect(ctx, actorExternalID, peerExternalID, encryptionMode *string)` ở service và `(ctx, creatorID, peerID, encryptionMode *string)` ở repository. Summary/list/create có encryption_mode. `Send(ctx, actorExternalID, threadExternalID, messageID, contentFormat, content)` ở service và `(ctx, threadExternalID, senderID, messageID, contentFormat, content)` ở repository; format/content đi tới response/history/event, retry trả UUID/seq cũ, publish lỗi 503 giữ payload.
3. Package Go internal/e2ee, bridge globalThis.MiniHermesE2EE sáu hàm, make wasm và hai asset routes vẫn là đầu ra Prompt 1. Prompt 4 dùng đúng JSON bridge II.7; không đổi crypto/AAD/envelope/chữ ký libsignal hoặc tạo crypto JavaScript. Binary/runtime sinh từ build/ignored, không chứa khóa user.
4. Web hiện chỉ có guard tạm `hasEncryptedMessages`/`canSendPlaintext`, placeholder trong renderMessages và read gate trong renderedReadSequence. Prompt 4 thay guard E2EE bằng readiness/ownership/state và plaintext view đã decrypt/local commit; giữ canUseThread, REST/WS/cursor/reconnect/membership snapshots. Raw message.content vẫn opaque. Same-UUID payload conflict guard của contract chưa được thêm vào realtime-core; cần làm khi nối async decrypt ở Prompt 4.
5. Chưa có web/e2ee-wasm.js, e2ee-state.js, e2ee.js hoặc UI E2EE. Prompt 4 triển khai loader/six-method bridge, IndexedDB scope API URL + user UUID, Web Locks owner/other_tab và serialize state; save keys trước upload, pending payload trước POST, retry không claim/seal lại, cache message key + xóa OPK private trong một local transaction. Chỉ decrypt/commit/render plaintext xong mới cho read marker.
6. Thêm thao tác web init/restore/upload/refill/chọn E2EE direct mới/fingerprint/pending theo II.8–9; demo A→B và failure checks thuộc Prompt 4. Dùng browser thật cho IndexedDB/Web Locks, script tạm rồi xóa, ghi kết quả thực tế. B→A và ADR #4 thuộc Prompt 5. Không suy browser/demo PASS từ backend hoặc Node guard của bước này.

## Hoàn tất Prompt 4 — 04/10/2026

**Web A→B đã chạy thành công trên browser thật.** Giữ branch `feat/e2ee`, HEAD `b5260e8fa902bd07c93763ab72022abc4adf52ff`, cùng toàn bộ thay đổi chưa commit Prompt 1–3. Đã đọc AGENTS.md, README, contract/evidence/đầu vào Prompt 4, Prompt 4 nguồn và đối chiếu implementation crypto/bridge/API/thread/message/event/gateway/web trong working tree. Không suy code từ HEAD, không checkout/reset/merge/stage/commit/push và không tự triển khai Prompt 5.

### Files và hành vi đã triển khai

| Files của Prompt 4 | Thay đổi |
| --- | --- |
| `web/e2ee-wasm.js` — mới | Loader loading/ready/error, fetch/instantiate Go WASM, giữ runtime sống, gọi đúng sáu method JSON bridge; lỗi artifact/runtime chặn E2EE |
| `web/e2ee-state.js` — mới | IndexedDB `mini-hermes-e2ee` v1, `profiles`, compound key `[api_url,user_id]`; clone trước put, resolve sau transaction.oncomplete, abort giữ state cũ |
| `web/e2ee.js` — mới | Owner Web Lock theo scope, hàng Promise, bootstrap validate local keys/profile/pending, init/upload/refill, TOFU/fingerprint, seal/open/cached decrypt, exact pending/retry/cancel và close |
| `web/app.js` | UI E2EE nối REST/WS/history/catch-up hiện có; raw ciphertext + plaintext view riêng trong RAM; async decrypt/local commit/render, guard conflict, read marker và callback theo session/membership |
| `web/realtime-core.js` | Validate cả page trước merge; same UUID khác seq/context/kind/format/content hoặc cùng seq khác UUID bị từ chối, giữ payload cũ |
| `web/index.html`, `web/style.css` | Load script theo thứ tự, trạng thái account/message, init/upload/refill/takeover, chọn mode direct mới, fingerprint và thao tác pending/giải mã lại |
| `internal/route/router.go` | Chỉ bổ sung ba asset routes JS riêng và no-store; API endpoint/signature/wiring đã có giữ nguyên |
| `docs/e2ee-demo.md` — mới, `docs/e2ee-contract.md` | Cách chạy, evidence phân biệt môi trường và đầu vào Prompt 5; phần II cố định giữ nguyên văn nguồn |

Tổng cộng **10 files** của lượt này. Không sửa crypto Go, bridge Go/WASM, API handlers/services/repositories, gateway/ticket/membership, SQL/generated code/schema/migrations, dependencies, Makefile, AGENTS.md hoặc nguồn prompt. Không thêm permanent test; đúng sáu test files được phép vẫn là toàn bộ danh sách. Artifacts WASM/runtime được build lại và vẫn ignored. SHA-256 **73 files bảo vệ** chụp đầu lượt giữ nguyên; phần II contract giống phần II nguồn, runtime SHA-256 khớp `GOROOT/lib/wasm/wasm_exec.js` từ compiler hiện tại.

- Private IK/SPK/20 OPK và pending_upload được commit trước upload; signature SPK sinh/lưu một lần. Restore derive public qua Go từ private đã có, verify signature/profile/pending, không init trong bootstrap hoặc reset state malformed. Refill 20 OPK tăng ID đơn điệu; pending upload retry nguyên public body. Registration/watermark chỉ xác nhận sau response khớp và local commit.
- Chỉ owner đọc private profile và dùng E2EE. Non-owner `other_tab` có placeholder/metadata và nút tiếp quản chủ động; không claim/send/decrypt/read E2EE. Promise queue serialize mọi mutation. Logout hủy HTTP, bỏ callback stale, clear RAM/DOM, chờ local transaction/task rồi nhả lock; durable profile còn để đăng nhập lại. Thiếu browser APIs/secure context/WASM báo lỗi, không fallback.
- Send lấy context từ summary và actor, validate text trước claim; Go verifyBundle + TOFU + seal. Save message key/context/hash/public identities/pin/pending trước POST. Retry/reload chỉ POST nguyên body đã lưu, không claim, seal, UUID/nonce mới. 2xx và REST history chỉ clear pending khi payload exact; cancel giữ message key. Không persist draft/plaintext/JWT/password/EK private trong profile.
- Receive event/response/REST initial/older/catch-up chung pipeline: giữ raw content, normalize `message_id`/`id`, guard UUID/payload cả trong và giữa thread cache trước merge/unread, sort seq. Cached/own dùng decryptWithKey với expected hash/context; incoming mới dùng Open với private SPK/OPK đúng ID. **Một put/transaction** cache key/pin và xóa private OPK; chỉ sau oncomplete mới render plaintext bằng textContent. Decrypt/abort/save lỗi giữ OPK. Duplicate không thêm dòng/unread; hoàn tất decrypt vẫn render khi raw merge changed=false.
- Read marker chỉ sau decrypted view đã commit/render, thread active, tab visible và cuối viewport; bất kỳ loaded ciphertext pending/error/conflict hoặc thiếu owner đều chặn marker. Mode/peer chưa biết lấy summary bằng REST. Plaintext/group vẫn có guard riêng và giữ cursor/reconnect/heartbeat/ticket, không polling.
- Fingerprint từ Go cho own IK và peer pin đã biết, hiển thị UUID; không claim để xem. Identity thay đổi có lỗi rõ và không ghi đè pin. Web Locks chỉ cùng origin/profile, không nhiều thiết bị. Mất IndexedDB không thể khôi phục key cũ; WASM không bảo vệ keys khỏi XSS hoặc mã trong cùng trang.

### Lệnh và môi trường kiểm chứng thực tế

Windows amd64, Go `go1.27.1`, Node `v24.11.1`, Edge `154.0.4258.53` **headless browser thật qua CDP**, hai `--user-data-dir` độc lập; PostgreSQL `16.15`/Redis local đã chạy từ bước trước. Harness Go tạm dùng router/auth/JWT/handlers/services/repositories/Redis publisher/consumer/Hub/gateway thực, cổng loopback và stream/cache namespace ngẫu nhiên. Bốn tài khoản fixture mới; login/init/upload/tạo thread/send được thao tác qua UI. Go API harness không encrypt/decrypt; crypto chạy trong WASM ở Edge. Node điều khiển CDP và assert, không thay browser bằng DOM/IndexedDB/Web Locks mock trong demo này.

| Lệnh / kiểm chứng | Môi trường | Kết quả thực tế |
| --- | --- | --- |
| `make test` | Go native unit/crypto, sáu test files hiện có | PASS; các package test dùng Go cache, không gọi đây là integration browser/DB |
| `make build` | Go native | PASS |
| `make wasm` | Go compiler js/wasm + runtime cùng compiler | PASS |
| `go vet ./...`; `gofmt -l internal/route/router.go` | Go native | PASS; không file cần format |
| `node --check web/app.js`, `web/realtime-core.js`, `web/e2ee.js`, `web/e2ee-state.js`, `web/e2ee-wasm.js` | Node syntax | PASS cả năm files |
| `node .tmp-e2ee-controller.cjs` | Node + Go/WASM thật; storage/Web Locks/API mock | PASS **76 assertions**: save trước HTTP, A→B crypto, cached decrypt, abort/retry/restore/malformed/pin/refill/ownership; không browser evidence |
| Script Node stdin cho loader/storage | Node runtime/IndexedDB mock | PASS **14 assertions**: scope/clone/commit-abort/lifecycle/runtime exit; không browser evidence |
| `node .tmp-e2ee-p4-guard/web-guard.cjs` | Node VM chạy app/realtime thật, DOM/fetch/controller mock | PASS **83 assertions**: UUID/conflict/page atomic/unread, async render/read, blocked state/stale callback và plaintext/group; không browser evidence |
| `go build -o .tmp-e2ee-p4/browser-harness.exe ./.tmp-e2ee-p4/browser-harness`; chạy harness bằng Start-Process Hidden | Go native → PostgreSQL/Redis thật, API/gateway loopback | PASS build/start, fixtures isolated; chưa coi start là demo PASS |
| `node --check .tmp-e2ee-p4/browser-demo.cjs`; `node .tmp-e2ee-p4/browser-demo.cjs` | Node CDP → hai Edge profiles thật + API/DB/Redis/gateway thật | PASS **50 assertions** ở lượt đầy đủ cuối; các case thực tế bên dưới |
| `node --check .tmp-e2ee-p4/browser-followup.cjs`; `node .tmp-e2ee-p4/browser-followup.cjs` | Edge thật trên hai profiles/fixture cuối | PASS **13 assertions** bổ sung refill/upload retry/cross-thread UUID; tổng **63 browser assertions** |
| `go run ./.tmp-e2ee-p4/db-summary` | Go native → PostgreSQL thật, SELECT metadata | PASS: năm E2EE rows/seq 1–5/last_seq 5, read markers 5; OPK/watermark đúng bên dưới |
| `go run ./.tmp-e2ee-p4/cleanup-check` | Go native → PostgreSQL/Redis thật, chỉ đọc sau cleanup | PASS: cả tám fixture UUID accounts còn 0, keys trong hai namespace Redis còn 0 |
| SHA-256 files bảo vệ/runtime; phần II so nguồn; `git diff --check`; danh sách `*_test.go` | PowerShell/Git/tài liệu | PASS: giữ implementation các bước trước, runtime cùng compiler, hợp đồng cố định và phạm vi test |

### Các case đã chạy trên Edge thật

| Case | Bằng chứng thực tế |
| --- | --- |
| Hai client độc lập | A/B login, init/save/upload bằng UI; IndexedDB đúng version/store/keyPath/scope, có private state nhưng không persist plaintext; own fingerprint hiện |
| A→B qua WebSocket | A gửi từ form REST; frame gateway `message.created` tới B; Go/WASM Open, local key cache/OPK delete, render plaintext. Tin đầu seq=1, một dòng chat, B có một cached key và 19 private OPK |
| Network và PostgreSQL | POST `e2ee_v1` chứa JSON envelope, không chứa chuỗi plaintext của tin; SQL SELECT thực trả envelope/ciphertext. SHA-256 nguyên content DB bằng content POST; không log content/key/JWT |
| Sáu hàm bridge | Đếm callbacks tại boundary native Go/WASM trong Edge: generateKeyPair, signPrekey, verifyBundle, seal, open, decryptWithKey đều đã gọi với JSON contract; wrapper chỉ đếm tên hàm, không ghi input/output |
| Reload và REST | B reload cùng browser profile, restore private/cache; REST history decrypt bằng cached key sau OPK delete. Plaintext không lấy từ persisted draft/cache plaintext |
| Offline/reconnect | B được đặt offline qua CDP và socket đóng khi A gửi; bật mạng/socket reconnect thì REST catch-up decrypt được tin thiếu. Phân biệt frame WS ban đầu với request GET history/catch-up |
| Web Locks | Tab A thứ hai cùng profile/API/user hiện other_tab; không có profile private/decrypt/send. Owner logout clear RAM/DOM; tab hai bấm takeover lấy lock và decrypt lịch sử durable |
| Retry sau mất kết quả | Fixture che response POST 2xx thật bằng 503 sau backend commit/publish; A giữ pending, reload rồi retry. Body hashes giống nhau, UUID/content nguyên, claim count không tăng; DB chỉ một row seq=3 và last_seq=3 ở checkpoint retry |
| Refill/upload retry | B bấm refill: private batch 20 OPK IDs 22–41 và pending upload đã commit IndexedDB trước HTTP. Che response upload 200, reload rồi retry nguyên public body/signature SPK, không regenerate; server watermark 41, không dùng count làm ID |
| Abort khi gửi | Instrument native IDBObjectStore.put rồi transaction.abort tại pending-save: không POST message, không commit message key/pending mới. OPK public đã claim có thể hao, đúng đánh đổi |
| Abort khi nhận/read | Transaction nhận bị abort sau put: private OPK giữ nguyên, không cache key mới, không render plaintext hoặc vượt read marker. Gỡ lỗi rồi reload/REST decrypt thành công, commit key+OPK delete trước render/read |
| Duplicate/cache | Event/REST trùng UUID không thêm dòng/unread hoặc consume private OPK lần hai; reload dùng cached decrypt; read tăng sau render, không placeholder |
| Pin thay đổi | Pin fixture khác hợp lệ được sinh từ Go ở browser memory; incoming bị chặn, không ghi đè pin/cache/xóa OPK/vượt marker. Khôi phục pin fixture, reload REST decrypt được; không phải rotation/reset keys sản phẩm |
| Logout/account scope | Đăng nhập C trên tab A cũ không đọc keys/plaintext của A; quay lại A bị lock owner hiện tại chặn. Callback/session cũ không chuyển view sang user mới |
| Plaintext/group | UI tạo/gửi/render direct plaintext và group/system/text vẫn thành công; lỗi nền E2EE không chặn plaintext composer |
| Readiness thất bại | Các trang browser thật được inject thiếu Web Locks, thiếu IndexedDB hoặc chặn asset WASM: lỗi rõ, chặn init/upload/E2EE, không fallback; mở direct plaintext và composer vẫn sẵn sàng |
| UUID qua thread | Follow-up đưa cùng UUID sang cache thread khác: bị từ chối trước cập nhật unread/seq/event/raw maps, không ghi đè ciphertext/khóa cũ |

Hai lần thất bại ban đầu thuộc helper automation: bấm login trước deferred app handlers và đọc DevToolsActivePort cũ khi khởi động lại profile. Helper đã sửa chờ app và port mới, rồi chạy lại đầy đủ trên fixture mới đạt 50 assertions; không dùng lượt lỗi hoặc build/Node mock để suy browser PASS. Mất response được mô phỏng ở HTTP wrapper **sau backend 2xx/DB commit**, không mô phỏng TCP disconnect/server crash.

Checkpoint cuối gồm **63 assertions browser thật** (50 suite + 13 follow-up). DB E2EE có **5 rows, seq 1–5/last_seq 5**, sáu POST attempts cho năm UUID; UUID retry có hai body hashes giống nhau. Sáu claims gồm một OPK hao do pending-save bị abort trước POST. A public OPK 20/watermark 21/SPK 1; B public OPK 34/watermark 41/SPK 1, private OPK local 35 và cached message keys 5. Private OPK local nhiều hơn public một khóa vì lượt claim hao đó chưa có message để B consume local. Read markers hai phía đạt 5 sau commit/render. Refill không thay IK/SPK/signature và watermark 41 không phải số khóa còn lại.

**Cleanup đã hoàn tất:** hai harness đã dừng API/gateway/consumer/Hub/connections và xóa riêng threads/users fixture với cascade participants/messages/prekeys; kiểm tra độc lập cả tám UUID và hai stream/cache namespaces còn 0 user/key. Chỉ ticket keys do helper cấp được dọn, không chạm namespace ứng dụng khác. Tất cả Edge processes sở hữu đã thoát; resolve/check chính xác `.tmp-e2ee-p4` trong workspace rồi xóa helper/exe/log/private fixture JSON và hai profile IndexedDB tạm. Node helpers controller/guard đã xóa, loader/storage dùng stdin. Không reset/TRUNCATE/reseed sequence hoặc tắt PostgreSQL/Redis có sẵn. Profile/user người dùng ngoài fixture giữ nguyên; không lưu secrets/plaintext vào evidence.

**CHƯA KIỂM CHỨNG:** B→A, full Prompt 5/failure cuối tuần và ADR #4; browser khác/mobile/Unix; browser crash/power loss, storage eviction/quota thật; runtime Go lệch phiên bản thực tế; khôi phục khóa hoặc nhiều thiết bị ngoài phạm vi. Failure storage đã kiểm chứng bằng native transaction.abort, không gọi đây là quota/power-loss PASS. Runtime thoát ngay được kiểm tra bằng Node mock riêng. Chưa có review sơ đồ của mentor. Giới hạn race detector của bước trước vẫn giữ nguyên.

### Đầu vào cụ thể cho Prompt 5

1. P1–3 backend/crypto/bridge giữ nguyên và web A→B đã chạy như evidence trên. Không cần chọn lại schema/API/crypto, tái triển khai prekeys/ciphertext hoặc sửa nguồn prompt. Sáu bridge method vẫn trên `globalThis.MiniHermesE2EE`; JS facade `MiniHermesWASM.load().call(method,input)` chỉ serialize contract.
2. Controller `globalThis.MiniHermesE2EEClient.create({apiURL,userID,request,onChange,current})` tại web/e2ee.js đã có `start/takeOver/initialize/upload/refill/send/retry/receive/cancelPending/close`, `snapshot/canRead/canSend`. API URL cùng origin, UUID từ JWT đối chiếu GET /users. Loader/state JS cùng script thường, ba asset routes/no-store đã có. Readiness/lock/state riêng với canUseThread/guard plaintext.
3. Profile v1, OPK IDs khởi tạo 2–21 và SPK 1, batch refill 20; restore validate qua Go, pending_upload/pending_send nguyên body. One pending_send toàn profile, local message key lưu cả hai public IK/context/hash; không cache plaintext. State receiver commit cache+pin/xóa private OPK bằng cùng transaction. Giữ schema II.8, không thêm store/version/migration.
4. app.js giữ raw messages theo seq/id, `decryptedViews` và `decryptJobs` ở RAM; mergeMessages/realtime-core guard conflict trước merge/unread. `processE2EEMessages` dùng nhận chung, re-render khi crypto xong; `renderedReadSequence` chặn loaded pending/error/conflict hoặc non-owner. Không đưa envelope ra UI hoặc đổi REST/WS/cursor/reconnect.
5. Prompt 5 phải dùng hai account/profile mới hoặc đúng state còn giữ của demo người dùng để chứng minh **B→A**: B claim bundle A, X3DH mới/ephemeral/nonce/UUID mới, A Open rồi local commit/render. Controller đã tổng quát sender/recipient nhưng chưa có evidence chiều trả lời. Fixtures tự động của bước này đã dọn; không dựa vào chúng để tiếp tục.
6. Còn kiểm chứng failure/regression toàn bộ theo Prompt 5 và viết `docs/adr/004-e2ee-without-double-ratchet.md`: X3DH mỗi tin, libsignal variant, no Double Ratchet/PCS đầy đủ, 3DH khi hết OPK, TOFU, cached keys và metadata; IndexedDB/XSS/JS-WASM bị thay có thể lộ keys, WASM không cách ly bí mật với cùng trang. Chỉ ghi PASS đã chạy; script web/bridge/gateway tạm phải xóa theo AGENTS.md.

## Hoàn tất Prompt 5 — 04/10/2026

**Demo hai chiều đã được kiểm chứng trên browser thật; tuần 5 hoàn tất trong phạm vi contract.** Giữ branch `feat/e2ee`, HEAD `b5260e8fa902bd07c93763ab72022abc4adf52ff` và toàn bộ thay đổi chưa commit Prompt 1–4. Đã đọc AGENTS.md, README, contract/evidence/đầu vào Prompt 5, Prompt 5 nguồn, demo và implementation Go/bridge/API/event/gateway/web trong working tree. Không suy code từ HEAD, không đổi branch/stage/commit/push/merge/reset DB hoặc triển khai ratchet/rotation/backup/multi-device/group E2EE. Các mục CHƯA KIỂM CHỨNG ở checkpoint cũ là trạng thái lịch sử; kết quả hiện tại nằm trong phần này.

### Files và quyết định bàn giao

| File của Prompt 5 | Kết quả |
| --- | --- |
| `docs/adr/004-e2ee-without-double-ratchet.md` — mới | Bối cảnh/quyết định/hệ quả, X3DH độc lập mỗi tin và chiều ngược lại; phân tích FS/PCS có điều kiện, OPK/3DH/IK-SPK bất biến, message-key cache, replay/key reuse, TOFU, metadata theo code/schema và giới hạn client web; dẫn nguồn Signal chính thức |
| `docs/e2ee-demo.md` | Hướng dẫn hai profile, bundle/refill/fingerprint, A→B/B→A, 4DH/3DH, reload/older/offline/REST catch-up, retry/owner/missing keys, query SQL đúng thread và evidence theo môi trường; giữ checkpoint Prompt 4 dưới nhãn lịch sử |
| `README.md` | Mô tả đúng E2EE web đang có, prekeys API/thread mode/message format/pending durable, build WASM/test scope, liên kết demo/ADR #4 và giới hạn hiện tại |
| `docs/e2ee-contract.md` | Trạng thái mới nhất, evidence Prompt 5 và bàn giao cuối tuần; phần II cố định giữ nguyên nguồn |

Không phát hiện lỗi implementation cần sửa ở lượt này. SHA-256 **93 files bảo vệ** giữ nguyên: crypto/bridge/backend/web/SQL/schema/dependencies/Makefile/AGENTS.md/nguồn prompt không đổi. Chỉ bốn file tài liệu trên thay đổi so đầu Prompt 5; không thêm test thường trực. Artifacts `web/e2ee.wasm` và `web/wasm_exec.js` được build lại, vẫn ignored và không chứa user keys. Sáu test files theo AGENTS.md vẫn là toàn bộ danh sách.

ADR đọc [X3DH](https://signal.org/docs/specifications/x3dh/) gồm §4.2–4.3 replay/key reuse và [Double Ratchet](https://signal.org/docs/specifications/doubleratchet/) gồm chain/DH ratchet, secure deletion/recovery. Quyết định giữ lượt X3DH mới mỗi message, B→A đổi vai sender; retry exact payload. Không tuyên bố bỏ ratchet làm mất mọi FS: kết luận 4DH/3DH phụ thuộc private keys nào bị lấy/xóa; lộ cả profile còn cached message keys làm lộ lịch sử đó. Fresh X3DH không phải cơ chế PCS đầy đủ, TOFU không xác thực lần đầu và WASM không bảo vệ key khỏi mã độc cùng trang. Chữ ký giữ nguyên **biến thể libsignal v0.2.2**, không tuyên bố tương thích đầy đủ Signal.

### Lệnh và môi trường kiểm chứng

Windows amd64, Go `go1.27.1`, Node `v24.11.1`, Edge `154.0.4258.53` headless qua CDP với **hai profiles độc lập**, IndexedDB/Web Locks thật. PostgreSQL `16.15`/Redis thật đã có; harness Go tạm dùng router/JWT/handlers/services/repositories/publisher/consumer/Hub/gateway thật, cổng và stream/cache namespace riêng. Node điều khiển Edge; crypto chạy bằng Go/WASM tại browser. Harness không mã hóa/giải mã phía server.

| Lệnh / kiểm chứng | Môi trường | Kết quả thực tế |
| --- | --- | --- |
| `go test -count=1 ./internal/e2ee ./internal/module/e2ee/service ./internal/module/thread/service ./internal/module/message/service ./internal/module/user/service ./internal/wsticket` | Go native crypto/unit, sáu package hiện có | PASS chạy mới, không cache; service mocks không thay PostgreSQL integration |
| `make test` | Go native `go test ./...` | PASS; cache sau lượt fresh test bên trên |
| `make build`; `make wasm`; `go vet ./...` | Go native + compiler js/wasm | PASS; runtime SHA-256 khớp `GOROOT/lib/wasm/wasm_exec.js` |
| `gofmt -l` phạm vi crypto/bridge/E2EE module và các file thread/message/route liên quan | Go formatter, chỉ đọc | PASS không file cần format trong phạm vi đã chọn; xem giới hạn kiểm tra rộng bên dưới |
| `node --check web/app.js`, `web/realtime-core.js`, `web/e2ee.js`, `web/e2ee-state.js`, `web/e2ee-wasm.js` | Node syntax | PASS cả năm |
| `node .tmp-e2ee-p5-loader.cjs` | Node + artifact/runtime Go thật, fault/mock riêng | PASS **14 assertions**; load/call crypto thật, lỗi import runtime bị từ chối; HTTP 404/bridge malformed dùng mock, không browser evidence |
| Build/chạy `.tmp-e2ee-p5/harness` bằng Go và Start-Process Hidden; `node --check` ba browser helpers | Go native → PostgreSQL/Redis/API/gateway thật; Node syntax | PASS build/start/syntax; start không tự chứng minh demo PASS |
| `node .tmp-e2ee-p5/browser.cjs` | Node CDP → Edge thật | **40 core assertions đạt**, command exit 1 ở helper timeout bước retry vì REST đã xác nhận pending; không ghi toàn bộ command PASS |
| `node .tmp-e2ee-p5/continue.cjs`; `node .tmp-e2ee-p5/followup.cjs` | Edge thật, cùng profiles/fixture đã giữ | PASS/exit 0: **12 + 11 assertions**, hoàn tất các case còn lại; tổng **63 browser assertions đạt** |
| `go run ./.tmp-e2ee-p5/db-summary` | Go native → PostgreSQL thật, SELECT metadata/hash | PASS checkpoint DB bên dưới, không log keys/JWT/secret/plaintext |
| `go run ./.tmp-e2ee-p5/cleanup-check` | Go native → PostgreSQL/Redis thật, chỉ đọc sau cleanup | PASS: 0 fixture users/threads, 0 owned namespace/ticket keys |
| Hash 93 files/runtime; phần II so nguồn; `git diff --check`; danh sách test/ignored artifacts | PowerShell/Git/tài liệu | PASS giữ phạm vi và đầu ra Prompt 1–4; không query đổi nên `make sqlc` không cần chạy |

Lượt `browser.cjs` đã chạy đúng 40 core assertions rồi helper mở thread qua REST, khiến pending được xác nhận và nút retry biến mất **đúng contract**. Continuation giữ nguyên DB/profiles, reload chưa mở thread để kiểm chứng thao tác retry riêng, rồi follow-up kiểm tra HTTP/DB cuối. Không reset fixture để tránh lỗi, không đếm bước timeout/chưa chạy là PASS. Native/Node/build và browser/DB được ghi riêng.

### Kết quả browser và PostgreSQL mới

| Case | Bằng chứng thực tế |
| --- | --- |
| Hai chiều trong cùng E2EE direct | A→B và B→A gửi từ UI/REST, nhận WebSocket/decrypt/commit/render; vai sender/recipient tổng quát. EK/nonce/UUID/key mới mỗi message, không dùng lại key nhận của chiều trước |
| Có OPK và hết OPK | **51 messages = 38 lượt 4DH + 13 lượt 3DH**. A gửi 27: 18/9; B gửi 24: 20/4. Claim trả null khi public OPK cạn, cả hai phía vẫn decrypt; không xóa DB keys bằng tay để giả exhaust |
| Ciphertext và seq | 51 UUID/EK/nonce khác nhau; seq 1–51, last_seq 51. POST không có plaintext/private/message key, SHA nguyên content cache/POST/DB khớp. Hai UI không trùng dòng; read markers cuối đều 51 |
| Reload/older/own key cache | Cả hai reload và tải latest page + older đủ 46 tin tại checkpoint, own/peer decrypt bằng cached keys sau OPK delete. A cuối có 51 cached keys khác nhau; B cache bị chủ động clear ở case thiếu state sau khi đã chứng minh checkpoint 46 |
| Exact pending retry | Response được che sau backend 2xx/commit; reload chưa mở history rồi nút retry POST nguyên UUID/body. Claim/Seal delta 0, không tạo row hoặc tăng seq; REST reconciliation xác nhận pending exact không POST lại |
| HTTP retry/conflict thật | Cuối lượt **56 POST attempts = 51 mới + 2 retry 200 + 3 conflict 409**. Retry body original ở seq 51 byte-identical; sửa ciphertext/nonce/format đều 409, row count/last_seq giữ 51, không Seal |
| WS/REST và duplicate | Nhận WS hai chiều; event trùng không thêm dòng. Gateway fixture dừng, mở thread lấy REST history/decrypt; restart gateway/WS mở thành công kích hoạt REST catch-up, không polling |
| Tamper/signature/identity/hash | Inject frame hoặc claim response/RAM pin/cache để đi qua receive/send pipeline, Go/WASM và UI thật: nonce/ciphertext/header/AAD context/hash/signature/pin sai bị từ chối, placeholder/lỗi, không plaintext/OPK delete/cache overwrite/read vượt lỗi |
| Header có OPK nhưng thiếu private | Frame mới chưa cache có OPK ID nhưng local private OPK không có: báo lỗi và chặn Open/fallback 3DH; reload khôi phục durable OPK không bị xóa bởi lỗi |
| Nhiều tab và close | Tab cùng scope thứ hai other_tab bị chặn; đóng owner giải phóng Web Lock, tab còn lại chủ động takeover và decrypt durable history |
| Thiếu IndexedDB state | Logout/clear IndexedDB có chủ đích bằng CDP rồi login: missing_keys/chặn E2EE, không tự init/upload/claim/thay IK/SPK đã đăng ký |

**53 claims/51 messages** gồm hai claim kiểm tra fault không có POST, hao OPK đúng đánh đổi. Public OPK cuối 0 ở cả hai user, `last_prekey_id` vẫn 21/SPK ID 1; watermark là ID cao nhất, không phải count. Không in message keys để chứng minh khác nhau: kiểm tra trong browser chỉ trả boolean/count. Mất response là wrapper sau HTTP thành công/DB commit, không TCP disconnect/server crash; bad frame/claim/pin/hash là fault injection client, không tuyên bố server xác thực AEAD.

### Evidence tái sử dụng có chủ đích

Code các bước trước giữ nguyên nên không chạy lặp toàn bộ:

- **Prompt 2 PostgreSQL thật, 1472 assertions:** upload/retry/refill/authorization, concurrent claims không trùng OPK, exhausted null, retry upload không hồi sinh OPK, old/new IDs/mismatch rollback/watermark. Đây là evidence lịch sử của repository/transaction hiện tại, không phải mock hoặc lượt mới Prompt 5.
- **Prompt 3 PostgreSQL/Redis/gateway thật, 563 assertions:** thread mode/header/format/history/membership/retry và XADD lỗi thật sau commit trả 503; retry đúng giữ row/seq rồi publish. Prompt 5 thêm exact pending/UI/HTTP retry trên browser, không gọi wrapper response 503 là Redis lỗi thật.
- **Prompt 4 Edge thật, 63 assertions:** offline/reconnect/catch-up, native IndexedDB transaction.abort khi send/receive giữ OPK/cache/read, save trước HTTP, refill/upload retry, duplicate/unread/cross-thread UUID, logout/account scope, missing Web Locks/IndexedDB/WASM và plaintext/group. Node 76/14/83 assertions của Prompt 4 vẫn là mock evidence riêng. Số 63 Prompt 4 khác 63 Prompt 5, không đếm lặp thành suite mới.

### Giới hạn và trạng thái cuối tuần

**CHƯA KIỂM CHỨNG:** storage quota/eviction tự nhiên, process crash/power loss; runtime thực tế dùng hai phiên bản Go khác nhau; browser khác/mobile/Unix; race detector. Native transaction.abort và chủ động xóa IndexedDB chỉ chứng minh fault tương ứng, không suy quota/crash PASS. Node thiếu import runtime/HTTP 404 mock không chứng minh runtime mismatch/browser 404 thực tế. Kiểm tra gofmt rộng phát hiện ba file có định dạng tồn tại trước lượt này: `internal/module/message/publisher/redis.go`, `internal/module/thread/handler/groups.go`, `internal/module/thread/membership/redis.go`; không sửa ngoài phạm vi. Chưa có mentor review sơ đồ.

**Cleanup hoàn tất:** harness đóng server/consumer/Hub/pool và thoát bình thường; SELECT/Redis kiểm tra độc lập còn 0 fixture users/threads/namespace/ticket keys, 0 owned harness/Edge processes. Resolve/check đúng `.tmp-e2ee-p5` trong workspace rồi xóa hai profiles, helpers/scripts/exe/log/private fixture JSON; Node loader helper cũng đã xóa. Giữ PostgreSQL/Redis và dữ liệu khác, không reset/TRUNCATE/reseed hoặc kill process người dùng.

**Bàn giao:** Prompt 1–5 hoàn tất theo phạm vi demo và các giới hạn được ghi rõ; ADR #4/demo/README khớp implementation, phần II và sơ đồ web giữ nguyên. Build artifacts bằng `make wasm`, dùng tài khoản/profile demo mới theo hướng dẫn vì fixture kiểm chứng đã dọn. Không còn đầu vào triển khai Prompt 6 hoặc thay đổi kiến trúc tự động; Double Ratchet, rotation, backup/recovery, multi-device và E2EE nhóm ngoài phạm vi.

