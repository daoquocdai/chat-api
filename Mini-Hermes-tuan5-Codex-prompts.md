# Mini-Hermes — thiết kế và prompt triển khai tuần 5: web E2EE Go/WASM

Mục tiêu cuối tuần: hai người gửi, nhận và giải mã trực tiếp trên web Mini-Hermes hiện có; PostgreSQL chỉ giữ ciphertext đối với direct E2EE; sequence diagram và ADR #4 giải thích giới hạn bảo mật.

Bản cập nhật ngày 04/10/2026 chuyển demo sang web Go/WASM theo yêu cầu người dùng. Giữ API, envelope, AAD, Go crypto và một lượt X3DH độc lập mỗi message; thay client/state và toàn bộ chuỗi prompt cho browser. Đây là tài liệu thiết kế, chưa chứng minh implementation hoặc browser demo đã hoạt động.

## I. Cách sử dụng và căn cứ từ code

Nguồn hiện nằm ở `Mini-Hermes-tuan5-Codex-prompts.md` tại thư mục gốc; `docs/week5-codex-prompts.md` chưa tồn tại. Nếu được đặt ở đường dẫn đó hoặc đính kèm khi gửi Prompt 0, dùng đúng bản nguồn đã được review, không duy trì hai profile khác nhau.

Prompt 0 tạo/đồng bộ `docs/e2ee-contract.md` từ **toàn bộ phần II**. Bảng thay đổi và đối chiếu code ở cuối contract giúp review; phần II ở hai tài liệu phải giống nhau. Xem sơ đồ trước code, sau đó gửi Prompt 1 → 5. Nếu review đổi contract thì cập nhật nguồn và các prompt cùng lúc.

### 1. Những gì đã có

Baseline trong nguồn ban đầu ngày 02/10/2026 là `b5260e8fa902bd07c93763ab72022abc4adf52ff`. Đối chiếu hiện tại vẫn trên branch `feat/e2ee`, cùng HEAD; không checkout/reset/merge. Trước lượt cập nhật tài liệu, hai file nguồn/contract chưa được Git theo dõi; chưa có implementation E2EE.

| Phần hiện có | Trạng thái tại HEAD | Việc tuần 5 cần làm |
| --- | --- | --- |
| users.identity_public_key, users.last_prekey_id | Cột và sqlc model đã có; user queries chưa có nghiệp vụ prekeys | Upload identity/SPK/OPK, watermark tăng |
| prekeys | Schema signed/one_time, public key, signature, namespace ID | Upload/claim và consume OPK trong transaction PostgreSQL |
| threads.encryption_mode | Schema plaintext/e2ee; direct insert luôn plaintext; summary chưa mang mode | Opt-in khi direct mới, không đổi mode cũ |
| messages.content_format/content | Schema plaintext/e2ee_v1, content TEXT; Send/retry hiện hardcode plaintext | Request format + opaque envelope, mode/header checks, retry bytes |
| Message response, message.created, gateway | Đã chuyển content_format/content opaque | Giữ nguyên delivery/snapshot/ticket; web giải mã |
| Web | REST send, WS receive, REST cursor history/catch-up; render trực tiếp content; read marker theo message render | WASM bridge, IndexedDB, owner lock, decrypt view và read sau plaintext |
| Realtime merge | Gộp id/message_id và seq, chưa guard cùng UUID cùng seq nhưng payload khác | Chặn conflict trước overwrite; xử lý crypto hoàn tất sau duplicate |
| Router/Makefile | Serve asset hiện có; test/build native, chưa WASM | make wasm, runtime cùng Go, các route asset cụ thể |
| Tài liệu Prompt 0 | Contract ban đầu đã có; chưa triển khai | Cập nhật đồng bộ contract, nguồn, sơ đồ và Prompt 0–5 cho web |

Schema PostgreSQL đủ: không cần bảng session/ciphertext hoặc migration mới. Không sửa migration cũ/reset DB. Schema/runtime IndexedDB ở client là state local mới, không phải migration PostgreSQL.

### 2. Vì sao giữ X3DH độc lập mỗi tin

X3DH dùng public bundle người nhận đã đăng ký để tính secret tại client; người nhận dùng private tương ứng để tính cùng secret. API không giữ secret.

Sau lượt khởi tạo phải xử lý replay/key reuse và việc thay khóa trước chiều gửi ngược. Tuần 5 chưa làm Double Ratchet; vì vậy mỗi tin là lượt X3DH mới: A→B claim bundle B và sinh EK/nonce mới; B→A claim bundle A và làm lại với vai trò đổi chỗ. Retry một tin giữ dữ liệu cũ, không phải một lượt crypto mới.

Đánh đổi vẫn là claim bundle và nhiều phép DH cho mỗi tin, envelope lớn, cache message keys để đọc lại lịch sử, thiếu ratchet/PCS đầy đủ. Đây là profile demo, không tuyên bố tương thích toàn bộ Signal.

Go/WASM giữ một implementation crypto Go dùng chung với backend validation. JavaScript hiện có làm REST/WebSocket/IndexedDB/UI qua bridge JSON nhỏ; không xây crypto thứ hai. Message mới hiển thị ngay sau recipient decrypt + local commit, lịch sử/lấy bù vẫn REST trên trigger hiện có, không polling.

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

## III. Chuỗi prompt để gửi Codex

Mọi prompt đọc contract và nguồn đã cập nhật cho **web Go/WASM**. Sau mỗi bước ghi kết quả thực tế ở cuối contract/demo; không đổi API/bridge/crypto để tiện triển khai. Nếu bước trước chưa có artifact hoặc kiểm chứng cần thiết, nêu đúng giới hạn thay vì giả định PASS.

### Prompt 0 — khóa thiết kế web, chưa code

```text
Chuẩn bị hoặc cập nhật thiết kế tuần 5 web E2EE trên branch hiện tại.
Đọc AGENTS.md, README.md, docs/e2ee-contract.md nếu có, toàn bộ nguồn
Mini-Hermes-tuan5-Codex-prompts.md hoặc docs/week5-codex-prompts.md,
web/app.js, web/realtime-core.js và internal/route/router.go.
Nếu nguồn được đính kèm thay vì ở repo, dùng bản đính kèm và ghi rõ nguồn.

Trong bước này chỉ sửa tài liệu contract và nguồn/prompt khi cần đồng bộ.
Không sửa Go/JavaScript/SQL/dependency/Makefile, không chạy migration.
Xác nhận branch, HEAD, thay đổi chưa commit; không checkout/reset/merge.

1. Chép đầy đủ phần II hiện hành vào docs/e2ee-contract.md: crypto Go,
   API prekeys/thread/message, envelope/AAD, Go function signatures,
   bridge JSON sáu method, IndexedDB/Web Locks, web flow và phase.
   Giữ rõ chữ ký là biến thể libsignal v0.2.2, không XEdDSA canonical.
2. Đối chiếu users/prekeys/mode/format/retry/seq/thread transaction,
   Redis event, web realtime/merge/read marker và asset routes hiện tại.
   Schema đủ thì ghi rõ; không thêm migration/session/ciphertext table.
3. Đọc nguồn Signal, nhất là X3DH replay/key reuse; giữ lượt X3DH độc lập
   mỗi message ở cả hai chiều, retry nguyên payload. Đọc Go/WASM,
   IndexedDB và Web Locks từ nguồn chính thức để chốt boundary local.
4. Sơ đồ dùng web thực tế: private IndexedDB trước public upload,
   consume OPK trong transaction DB, Go/WASM encrypt trước REST POST,
   event ciphertext qua Redis/gateway/WebSocket, REST history/catch-up,
   recipient decrypt và local commit trước render/PUT read.
5. Thêm đối chiếu code, bảng thay đổi thiết kế và trạng thái phase ở cuối.
   Nếu có khác baseline ảnh hưởng contract, báo điểm khác; không tự thay
   API, bridge hoặc crypto. Không đánh dấu implementation/demo là xong.
6. Toàn bộ Prompt 0-5 phải khớp đầu vào/đầu ra phase tại II.10:
   Go+bridge/build WASM -> prekeys API -> thread/message opaque ->
   web IndexedDB/Web Locks và A->B -> B->A/failure/ADR.
   Không có entry point, state file, command hoặc test native client.
   Web/bridge smoke chỉ bằng script tạm hoặc thủ công theo AGENTS.md.

Đầu ra: hai tài liệu đồng bộ, sơ đồ web và bảng thay đổi để người hướng dẫn
xem. Dừng trước triển khai. Chưa review hoặc chưa chạy browser/build/demo
phải ghi rõ, không ghi PASS dự kiến.
```

Đầu vào cho Prompt 1: contract web/bridge đã được người dùng hoặc người hướng dẫn xem. Nếu review đổi quyết định, cập nhật cả nguồn và contract trước code; không giữ một prompt yêu cầu profile cũ.

### Prompt 1 — crypto Go, test và bridge/build WASM

```text
Triển khai Prompt 1 theo docs/e2ee-contract.md và phần II nguồn hiện hành.
Đọc AGENTS.md, go.mod, Makefile và router. Không chọn lại crypto hoặc bridge.
Đầu vào: contract web đã review, chưa implementation E2EE.
Đầu ra: internal/e2ee, native crypto tests, bridge và make wasm/runtime/assets;
chưa endpoint prekeys/message E2EE hoặc tích hợp chat web.

Tôi cho phép sửa đúng quy tắc test trong AGENTS.md để thêm hai ngoại lệ:
internal/e2ee/crypto_test.go,
internal/module/e2ee/service/service_test.go.
Giữ bốn file cũ và quy tắc khác. Không thêm test thường trực ở cmd/web.

1. Tạo types.go/crypto.go đúng II.3/6/7 và signatures Go giữ nguyên.
   X25519 crypto/ecdh, HKDF-SHA-256 crypto/hkdf, AES-256-GCM,
   crypto/rand; pin go.mau.fi/libsignal v0.2.2, chỉ ecc để sign/verify.
   Không identity Ed25519 riêng, session/ratchet/store hay crypto JS.
   Đọc source ecc để gọi constructors đúng; không CreateKeyPair/debug log.
2. Raw key32 Base64 canonical padded cho JSON; 0x05+public32 khi ký/AAD;
   signature64; int64 ID dương; OPK thiếu null; đúng DH ordering,
   F32/zero salt/info, 3DH/4DH, không tự thêm zero DH4.
   Verify signature trước Seal; reject low-order qua ECDH kiểm tra.
3. AAD theo byte layout hiện hành, UUID bytes, identity keys/EK/prekey IDs;
   không seq/created_at. Strict envelope, 8 KiB và lengths, tamper fail.
   EncodeEnvelope chỉ marshal một lần; Open/DecryptWithKey validate text.
4. Tạo cmd/e2ee-wasm/main_js.go với //go:build js && wasm.
   syscall/js đăng ký globalThis.MiniHermesE2EE, sáu method đúng II.7:
   generateKeyPair, signPrekey, verifyBundle, seal, open, decryptWithKey.
   Một JSON string vào, một JSON string {ok,data,error} ra.
   Dùng DTO local/keypair/context snake_case, hash/fingerprint và error
   codes đúng contract; không đổi signatures Go hay wire HTTP.
   Helpers SHA-256/adapter private được phép; không thêm method crypto.
5. Bridge strict decode/max32KiB, safe errors, không panic/log input/output.
   Chỉ crypto/encoding; không HTTP, JWT, IndexedDB, DOM hoặc state giữ lại.
   Giữ js.Func sống; main không kết thúc khi browser dùng callbacks.
6. Thêm make wasm: GOOS=js GOARCH=wasm build web/e2ee.wasm;
   copy web/wasm_exec.js từ GOROOT/lib/wasm của cùng compiler.
   Ignore hai artifact Git; không tải runtime CDN/khác version.
   Router thêm riêng /e2ee.wasm MIME application/wasm và /wasm_exec.js,
   cache no-store như web hiện có. Chưa load runtime vào UI chat.
7. Test crypto trong crypto_test.go: 3DH/4DH, UTF-8, signature sai,
   AEAD/AAD/context/header tamper, low-order/malformed, RFC vectors phù hợp.
   Standard testing; không interface cho từng primitive để mock.
8. Bridge smoke bằng script/page tạm: chạy artifact với đúng runtime,
   gọi cả sáu method đúng JSON, compare native/wasm, verify lỗi an toàn.
   Build js/wasm riêng chưa chứng minh browser runtime; Node smoke cũng
   không được ghi thành browser PASS. Xóa mọi script/page fixture sau chạy.
9. gofmt, go mod tidy đúng dependency cần pin, make test, make build,
   make wasm. Go version giữ nguyên, không upgrade unrelated dependency.
   Nếu Make thiếu dùng Go/PowerShell tương đương và ghi lệnh/giới hạn.

Không sửa message/thread/prekeys nghiệp vụ, gateway hoặc viết web crypto.
Ghi version/license GPL-3.0 và evidence thật cuối contract; không keys/SK/log.
Không tuyên bố audited, XEdDSA canonical hoặc tương thích Signal đầy đủ.
```

Đầu vào cho Prompt 2: crypto Go và bridge JSON đúng contract, WASM/runtime build được; các check đã chạy hoặc giới hạn được ghi rõ. Chưa có API prekeys hoặc state web.

### Prompt 2 — API public prekeys và transaction

```text
Tiếp tục kết quả Prompt 1. Đọc AGENTS.md, contract, internal/e2ee.
Chỉ triển khai POST /e2ee/prekeys và POST /e2ee/bundles/:user_id/claim.
Giữ JSON/status/limits II.4; chưa tạo thread E2EE qua API hoặc tích hợp web.

1. Tạo internal/module/e2ee theo handler -> service -> repository.
   Tái sử dụng public wire types internal/e2ee; service không Gin,
   repository không JWT. Upload actor từ context JWT, không user_id request.
2. Handler decode strict riêng, max16KiB/quá giới hạn413; field lạ/trailing
   data bị từ chối, không bật global Gin setting và không log body/keys.
   Service validate key32/signature64/Base64 canonical/low-order,
   signature SPK, 0-100 OPK và IDs dương/không trùng kể cả SPK/OPK.
3. Thêm db/queries/prekeys.sql; make sqlc, không viết tay generated Go.
   Upload khóa user trước đọc/sửa, transaction lưu IK/SPK/OPK và watermark.
   Registered IK/SPK ID/public/signature phải giống, khác409/rollback.
   last_prekey_id là high-watermark chung; mới phải lớn hơn đầu transaction.
   ID cũ còn row phải khớp public; row đã claim mất thì bỏ qua, không hồi sinh.
   Signature retry giữ nguyên; không tombstone/reservation bảng mới.
4. Claim kiểm tra actor/recipient là hai active participant direct E2EE;
   không tự claim, không user ngoài thread. Mode409/quyền403/thiếu404.
   Trong transaction khóa recipient user, đọc IK/SPK, lấy OPK nhỏ nhất bằng
   DELETE RETURNING, commit xong mới trả bundle; hết OPK là field null.
   Upload/claim cùng row lock user; không Redis lock/SKIP LOCKED/hoàn trả.
5. Thêm E2EEHandler vào router và wiring cmd/main.go; giữ asset routes
   Prompt 1. Không nhận private key, message key, EK private hoặc plaintext.
6. Service test chỉ trong internal/module/e2ee/service/service_test.go
   đã được phép: validation, conflict, actor/claim access và lỗi repo.
   Mock không chứng minh concurrency/transaction; kiểm chứng DB thật sau.
7. make sqlc, gofmt, make test, make build, make wasm để xác nhận package
   crypto shared vẫn build js/wasm. Tool thiếu ghi đúng giới hạn.

Chưa có đường API tạo direct E2EE tới Prompt 3: không sửa mode DB để giả
hoàn tất claim demo. Không sửa migration/reset/TRUNCATE, message format,
web/gateway hoặc bridge. Cuối contract phân biệt endpoint với integration
chưa chạy; upload fixture riêng nếu môi trường có API/DB.
```

Đầu vào cho Prompt 3: hai endpoint/wiring đã có; claim chỉ dùng cho direct E2EE. Tạo thread E2EE và đường message được hoàn thành ở bước kế tiếp.

### Prompt 3 — thread mode và đường lưu ciphertext opaque

```text
Tiếp tục Prompt 2. Đọc contract, AGENTS.md, thread/message handler/service/
repository, queries và web/app.js. Triển khai mode/thread/message II.5.
Chưa tích hợp bridge, IndexedDB hoặc decrypt web.

1. CreateDirectRequest thêm EncryptionMode *string; plaintext/e2ee khi có.
   Omitted tạo plaintext nếu mới, trả actual mode nếu đã có.
   Explicit e2ee mới yêu cầu cả hai registered IK/SPK, thiếu409.
   Explicit khác mode cũ409; không tạo second pair/upgrade/migrate history.
2. Giữ khóa hai user internal ID tăng trong CreateOrGetDirect; kiểm tra
   IK/SPK trong transaction. Mode đi qua query/model/DTO/summary/list.
   Group plaintext, giữ read/unread/membership hiện có.
3. SendMessageRequest thêm content_format; Send handler/service/repository
   nhận contentFormat trước content. Cập nhật mọi interface/mock/caller.
   Omitted/empty plaintext; format không biết400, sai mode409.
   Plaintext giữ trim/rune rule; E2EE không trim/normalize/re-marshal.
4. LockThreadForParticipant trả ID/kind/mode dưới thread lock;
   recheck active membership, UUID retry rồi mới mode/header của tin mới,
   cấp seq/insert cùng transaction. Update các caller shape bị ảnh hưởng.
5. Header recipient là peer active; sender IK khớp actor; recipient SPK ID
   khớp registered stable SPK. OPK đã claim không cần còn row.
   Server parse/validate cấu trúc, không Seal/Open hay verify nội dung AEAD.
6. SQL insert dùng request content_format. Retry đúng UUID/sender/thread/
   kind/format/content bytes trả row/seq cũ, không increment; khác409 chung.
   Response/history/event nguyên opaque content/format; publish sau commit,
   snapshot và503 hiện có không đổi; không outbox.
7. Message body max16KiB, E2EE content8KiB; limits/encoding đúng II.5/6.
8. Guard web tạm cho backend phase: placeholder e2ee_v1/mode e2ee,
   chặn input/button/submit plaintext và không PUT read placeholder.
   Event trước summary nhận biết format; chưa biết mode fetch summary.
   Không đổi canUseThread làm mất history/group hoặc refactor realtime.
   Đây là trạng thái tạm để Prompt 4 thay bằng decrypt/send web đầy đủ,
   không phải profile cuối cùng chỉ đọc E2EE bằng client khác.
9. Test service trong file được phép: legacy/plaintext, mode/format,
   malformed envelope, retry bytes/conflict/503. DB smoke dùng public
   fixture: upload hai bundle -> tạo direct E2EE -> claim -> POST envelope.
   Smoke cấu trúc server chưa chứng minh browser decrypt.
10. make sqlc, gofmt, make test, make build, make wasm; node --check app.js
    nếu guard web đổi. Không migration/reset DB; dùng account mới nếu
    cặp cũ đã có plaintext thread.

Không thay bridge/API/crypto, gateway/delivery/cache/ticket; không ghi demo
web PASS. Cập nhật evidence cuối contract, ghi rõ web tích hợp ở Prompt 4.
```

Đầu vào cho Prompt 4: backend upload/claim/direct E2EE/message opaque hoàn chỉnh; bridge/runtime từ Prompt 1; web giữ an toàn bằng placeholder tạm nhưng chưa có decrypt/state E2EE.

### Prompt 4 — web IndexedDB, Web Locks và demo A→B

```text
Tiếp tục Prompt 3. Đọc contract II.7-10 và code web/realtime/router.
Tích hợp Go/WASM trực tiếp vào web Mini-Hermes, chứng minh A->B trước.
Giữ REST send, WebSocket receive và REST history/reconnect/gap catch-up.
Không crypto JavaScript, client ngoài web hoặc thay endpoint/bridge.

1. Thêm web/e2ee-wasm.js, e2ee-state.js, e2ee.js; wire index.html/app.js
   và route asset JS riêng; style tối thiểu. Runtime/browser loader chờ
   đủ sáu method bridge, không await go.run kết thúc. Missing/mismatch
   artifact lỗi rõ, chặn E2EE nhưng không hỏng plaintext/group.
2. Gọi đúng JSON string bridge {ok,data,error} II.7. DTO local keys Base64
   raw32, context snake_case; verifyBundle/seal/open/decryptWithKey và
   hash/fingerprint đúng contract. Không đưa local keypair vào HTTP.
3. IndexedDB mini-hermes-e2ee v1/profiles, keyPath api_url+user_id.
   Scope normalize URL và UUID, token/password vẫn ở cơ chế session cũ.
   Load validate version/public-private match/registration/pending.
   Profile mất/malformed hiện missing_keys, không reset hoặc thay IK/SPK.
4. Lấy Web Lock exclusive ifAvailable theo name II.8, giữ suốt phiên.
   Non-owner other_tab không đọc private state hoặc send/refill/retry/
   decrypt/read marker E2EE; plaintext/group vẫn dùng. Có thử tiếp quản
   chủ động, không steal/poll/BroadcastChannel keys/multi-device.
   Serialize mọi mutation bằng một hàng Promise trong owner tab.
5. Sau login restore profile. Account mới có thao tác khởi tạo rõ:
   generate IK/SPK/20 OPK, sign SPK một lần, commit private/signature/
   pending_upload trước POST. Public registration/refill/retry đúng II.8;
   200 response verify rồi local commit mới bỏ pending. ID/watermark
   tăng, không regen signature/batch khi pending; 409 không reset keys.
6. Chọn E2EE khi tạo direct mới; mở cặp cũ actual mode, không upgrade.
   Ready/initializing/missing_keys/other_tab/error và lỗi từng message
   hiện rõ; button/input/submit riêng cho E2EE, canUseThread/group giữ đúng.
   Fingerprint Go-returned của own IK/peer pin; không claim chỉ để xem.
7. Send: validate text trước claim, UUID một lần, verifyBundle+pin,
   seal qua Go/WASM, nguyên content string, outer body stringify một lần.
   Commit key/context/hash/identity public/pin/pending_send trước POST.
   Save lỗi không POST; response2xx verify exact rồi commit bỏ pending.
   Pending global một profile chặn send E2EE mới. Retry/reload chỉ POST
   nguyên body; không claim/generate/Seal lại. UI gửi lại/đối chiếu/hủy
   pending UUID; cancel giữ key vì DB có thể đã commit; không persist draft.
8. Một receive pipeline cho event/response/REST initial/older/catch-up.
   Giữ raw ciphertext, normalize UUID, guard UUID/context/payload conflict
   trước merge, sort seq. Khi raw merge changed=false nhưng decrypt xong,
   vẫn render view. Mode/peer chưa biết fetch summary, không dùng envelope
   tùy ý làm context. WASM/state chưa ready giữ ciphertext chờ trong RAM.
9. Own/cached message: context đúng, decryptWithKey expected hash.
   Received mới: check pin, SPK/OPK đúng ID, open; 3DH chỉ khi null header.
   Commit cached key/context/hash/public/pin và xóa OPK private trong
   cùng transaction local; oncomplete rồi render plaintext textContent.
   AEAD/abort/quota/save lỗi không mất OPK hoặc key/pending đã commit.
   Không await HTTP/crypto trong transaction IndexedDB.
10. Preserve cursor pages/backoff/heartbeat/ticket/gap catch-up, không polling.
    Read marker chỉ plaintext đã decrypt/commit/render, active/visible/
    bottom; không vượt pending/error trong loaded messages, không summary.
    Logout/đổi account hủy task HTTP, chặn stale callbacks, clear RAM/DOM,
    settle local txn rồi nhả lock, giữ durable profile cho login lại.
11. Script tạm hoặc browser thủ công: JSON bridge, save failure,
    retry exact/reload, same UUID event+REST, duplicate/cache sau OPK delete,
    pin đổi, scope/account/logout, owner/other_tab, readiness và read marker.
    Dùng browser thật cho IndexedDB/Web Locks; xóa script/page fixture.
    Không thêm test thường trực cho web/cmd hoặc mock browser thành PASS.
12. API/DB/Redis/gateway thật và hai account mới trên hai tab web cùng origin:
    initialize/register cả hai -> mở direct E2EE -> A gửi từ UI ->
    B nhận WS và thấy plaintext; query DB ciphertext không plaintext.
    B reload cùng profile, mở lịch sử REST vẫn decrypt. Khi offline/reconnect
    REST catch-up tiếp tục; ghi evidence riêng event và REST.

Chạy make test/build/wasm; node --check JS mới/đổi. Ghi cách chạy và kết quả
A->B thực tế vào docs/e2ee-demo.md, giới hạn nếu môi trường thiếu.
Không tuyên bố B->A/failure cuối tuần xong; Prompt 5 kiểm chứng chiều trả lời.
```

Đầu vào cho Prompt 5: web A→B đã được chứng minh hoặc có danh sách CHƯA CHẠY chính xác; cùng controller/bridge đã có send/receive cho cả tài khoản. Chưa tự suy ra B→A PASS.

### Prompt 5 — web B→A, failure cases và ADR #4

```text
Tiếp tục Prompt 4. Đọc contract, AGENTS.md, e2ee-demo và code web/WASM.
Hoàn tất kiểm chứng hai chiều, sửa tối thiểu trong contract và viết ADR #4.
Không đổi API/bridge/crypto, thêm Double Ratchet hoặc đồng bộ nhiều thiết bị.

1. Xác nhận A->B qua web. B reply từ UI phải claim bundle A, EK/nonce/UUID
   mới và lượt X3DH mới; không dùng key nhận từ A. A thấy plaintext từ WS.
   Hai account reload cùng scope/profile, REST history own/peer decrypt.
2. PostgreSQL/HTTP thật với fixture riêng:
   - Concurrent claims cùng user không trả cùng OPK đã commit.
   - Retry upload sau claim không hồi sinh ID; watermark không giảm;
     existing OPK public khác409 và rollback.
   - Hết OPK trả null, web send/decrypt theo3DH; giới hạn FS ghi rõ.
   - Retry UUID/body same row/seq; sửa ciphertext/nonce/format409,
     last_seq không tăng. Claim lost response có thể phí OPK, không hoàn trả.
   - Redis503/network sau DB commit giữ pending; retry body byte-identical
     sau reload, không claim/seal lại; REST có thể xác nhận message đã lưu.
3. Browser thật:
   - WS realtime và REST initial/older/reconnect/gap cùng decrypt path;
     duplicate UUID event+REST không double consume/overwrite/unread.
   - B offline lúc gửi; reconnect WS thành công rồi REST catch-up decrypt.
     Gateway tạm không có event không làm mất lịch sử REST khi mở thread;
     không đổi sang polling để demo. Sender lấy own qua response/history.
   - AEAD/header/context/hash/pin tamper fail; không render plaintext,
     mất OPK/keycache hoặc vượt read marker; cache cũ không ghi đè.
   - IndexedDB abort/quota/commit lỗi ở upload/pending/decrypt không làm
     bước HTTP/render tiếp theo được coi thành công. Reload retry/history.
   - Hai tab cùng account: đúng một owner, other_tab bị chặn E2EE;
     owner logout/close cho tab kia tiếp quản chủ động. Hai UUID khác
     chạy cùng lúc; stale task không ghi nhầm profile/account.
   - Thiếu Web Locks/IndexedDB/WASM, artifact404/runtime mismatch báo rõ,
     không fallback key store hoặc crypto JS. Account mất state báo
     missing_keys, không tự tạo identity thay cho account đã đăng ký.
   - Read marker chỉ sau plaintext decrypt/local commit/render và điều
     kiện visible/active/bottom; placeholder/error không được tính đọc.
   - Plaintext/group/send/retry/pagination/membership/unread trước đó
     tiếp tục hoạt động; E2EE không bị guard tạm chặn gửi vĩnh viễn.
4. Script tạm cho bridge/browser/HTTP/DB rồi xóa theo AGENTS.md.
   Chỉ test thường trực trong bốn file cũ và hai ngoại lệ Prompt1.
   Không reset/TRUNCATE, đổi mode lịch sử, log keys/plaintext hay
   giết process người dùng; dùng fixture/port/stream riêng khi giả lỗi.
5. ADR docs/adr/004-e2ee-without-double-ratchet.md theo II.11:
   X3DH độc lập mỗi tin, FS/PCS/3DH/SPK cố định/local message-key cache,
   TOFU/first-contact MITM, replay/key reuse và metadata.
   Bổ sung IndexedDB compromise, XSS và JS/WASM bị thay có thể lấy khóa.
   WASM không tự bảo vệ keys khỏi mã độc cùng trang; Web Locks chỉ
   điều phối tab, không chống XSS/cross-profile/multi-device.
6. Hoàn thiện docs/e2ee-demo.md bằng thao tác web: môi trường/artifact Go
   runtime/browser support, register/login, keys/refill/fingerprint,
   chọn direct E2EE, hai chiều, WS/REST/reload/reconnect/pending/owner.
   Query ciphertext chỉ thread fixture. Bảng từng tình huống PASS/FAIL/
   CHƯA CHẠY và lệnh/evidence; không ghi token/private/secret/plaintext thật.
7. make sqlc nếu query đổi, gofmt, make test/build/wasm, node --check.
   Git diff đúng phạm vi, không state/secret/script/binary/runtime tracked
   hoặc migration cũ đổi. Không push/merge/checkout nếu chưa được yêu cầu.

Thiếu browser/services/tool thì ghi CHƯA CHẠY và cách tái hiện. Native/Node/
build/mock không thay integration/browser. Nếu tìm lỗi sửa theo contract
rồi chạy phần liên quan; không thêm worker/session cache/framework.
Đầu ra: web demo hai chiều hoặc giới hạn chính xác, DB ciphertext, ADR #4,
contract và implementation khớp; tóm tắt evidence thật và phần chưa chạy.
```

## IV. Tiêu chí nhận kết quả

| Hạng mục | Bằng chứng cần có |
| --- | --- |
| Thiết kế trước code | Contract/nguồn phần II giống nhau, sơ đồ web và bảng thay đổi, đã được xem trước Prompt 1 |
| Crypto Go | Native tests 3DH/4DH/signature/tamper/context, dependency đúng version/license |
| Go/WASM | make wasm, runtime cùng compiler, sáu method JSON bridge thực chạy; browser/Node ghi riêng |
| Public prekeys | DB chỉ public material; concurrent claim không trùng OPK, retry upload không hồi sinh key |
| Web local state | IndexedDB riêng API/user, private/pending trước HTTP, cache+OPK delete atomic, save failure |
| Ownership | Một tab owner per scope; other_tab chặn E2EE, takeover/logout/scope được kiểm chứng |
| Một chiều A→B | Hai tab web, sender REST POST, recipient WS decrypt/render; DB ciphertext |
| Hai chiều B→A | B claim bundle A và lượt X3DH mới, A WS decrypt bằng cùng bridge |
| Event và REST | History/older/reconnect/gap giải mã cùng pipeline, duplicate UUID không double consume/overwrite |
| Retry/reload | Same pending body byte-identical, UUID/seq một row; own/peer history dùng cached keys |
| Read marker | Chỉ sau plaintext decrypt/local commit/render; placeholder/error/other_tab không read |
| Tương thích | Plaintext/group/membership/unread trước đó vẫn hoạt động, không chuyển polling |
| ADR #4 | FS/PCS/replay/TOFU/metadata, IndexedDB/XSS/JS-WASM compromise và giới hạn Web Locks |
| Báo cáo | PASS/FAIL/CHƯA CHẠY theo evidence thực, không suy từ build/mock thành browser demo |

Query demo **chỉ đọc**, giữ schema và ciphertext format hiện có:

```sql
SELECT
    t.external_id AS thread_id,
    t.encryption_mode,
    m.external_id AS message_id,
    m.seq,
    m.content_format,
    m.content::jsonb ->> 'ciphertext' AS ciphertext,
    m.content::jsonb ->> 'ephemeral_key' AS ephemeral_public_key,
    m.content::jsonb ->> 'nonce' AS nonce
FROM messages AS m
JOIN threads AS t ON t.id = m.thread_id
WHERE t.external_id = :'thread_id'::uuid
  AND m.content_format = 'e2ee_v1'
ORDER BY m.seq;
```

Trong psql dùng `-v thread_id='<UUID thread demo>'`; query ciphertext không chứng minh crypto/browser an toàn. Cần demo hai chiều và failure checks. Không đưa JWT/private state/key/plaintext thật vào tài liệu hoặc output debug.

## V. Kiểm chứng: nguồn ban đầu và lượt cập nhật tài liệu

Các kết quả sau được **nguồn ban đầu kể lại**, không phải check vừa chạy khi đổi sang web:

- Review remote/schema/handler/service/repository/SQL/web tại baseline phần I; nguồn nói checkout review không bị sửa.
- Nguồn nói go test ./... và go build ./... trên baseline đã qua.
- Nguồn nói probe độc lập ngoài repo với Go 1.27.1 và libsignal v0.2.2 đã kiểm tra signature, 3DH/4DH, AES-GCM, tamper AAD/ciphertext và reject low-order.
- Probe chỉ nói về primitive/cách gọi thư viện; không có evidence bridge/browser/IndexedDB/Web Locks hoặc E2EE end-to-end trong repo.

Lượt 04/10/2026 chỉ cập nhật contract và nguồn/prompt, đọc lại code hiện có và nguồn Go/WASM/IndexedDB/Web Locks. Chưa build WASM, chạy browser/native tests/demo/integration hay migration. Toàn bộ phase implementation Prompt 1–5 và browser demo đều CHƯA TRIỂN KHAI / CHƯA CHẠY; chưa có xác nhận người hướng dẫn đã xem sơ đồ mới.

## VI. Bảng thay đổi thiết kế — 04/10/2026

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

Trạng thái: chỉ tài liệu đã được cập nhật trong lượt này. Sơ đồ web nằm tại II.2; Prompt 0–5 và phase tại II.10 cùng profile. Build/runtime/browser/demo/failure checks còn CHƯA CHẠY; không có implementation mới.
