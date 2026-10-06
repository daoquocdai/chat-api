# Luồng X3DH cho direct E2EE

Go/WASM thực hiện X25519, HKDF-SHA-256 và AES-256-GCM. IK là khóa định danh; SPK là prekey có chữ ký; OPK được cấp một lần; EK là khóa tạm mới cho mỗi tin; SK là message key 32 byte.

## Đăng ký và claim bundle

Hai tài khoản đã đăng ký IK/SPK và có direct `encryption_mode=e2ee`. API xác thực bằng JWT; claim nhận body `thread_id` và kiểm tra cả hai participant đang hoạt động.

```mermaid
sequenceDiagram
    participant A as Client A
    participant API as chat-api
    participant DB as PostgreSQL
    participant B as Client B
    B->>B: Tạo IK, SPK, 20 OPK, ký SPK
    B->>B: Commit private keys và pending upload vào IndexedDB
    B->>API: POST /e2ee/prekeys với public bundle
    API->>DB: Khóa user, lưu public keys và chữ ký
    DB-->>API: Commit thành công
    API-->>B: Xác nhận đăng ký
    A->>API: POST /e2ee/bundles/:user_id/claim với thread_id
    API->>DB: Kiểm tra quyền, DELETE RETURNING một OPK
    DB-->>API: Commit bundle
    API-->>A: IK, SPK, chữ ký và OPK nếu còn
```

Claim chỉ trả sau commit. Hết OPK trả `one_time_prekey=null` và dùng 3DH; có OPK dùng 4DH. Private OPK giữ ở recipient tới khi giải mã thành công. IK/SPK bất biến; upload retry không khôi phục OPK đã cấp.

## Gửi và giải mã

```mermaid
sequenceDiagram
    participant A as Client A
    participant API as chat-api
    participant DB as PostgreSQL
    participant RT as Redis Stream và gateway
    participant B as Client B
    A->>A: Xác minh SPK/pin IK, tạo EK và dẫn xuất SK
    A->>A: AES-256-GCM với nonce 12 byte và AAD
    A->>A: Commit message key và pending body vào IndexedDB
    A->>API: POST /threads/:id/messages với UUID và envelope
    API->>DB: Kiểm tra quyền/header/retry, cấp seq và lưu
    DB-->>API: Commit thành công
    API->>RT: XADD message.created
    API-->>A: Message sau publish thành công
    alt B online
        RT-->>B: WebSocket message.created
    else B bỏ lỡ event
        B->>API: GET /threads/:id/messages
        API-->>B: Ciphertext và metadata
    end
    B->>B: Dùng cached key hoặc tính SK từ private prekeys
    B->>B: Giải mã, commit key/pin và xóa private OPK đã dùng
    B->>B: Render plaintext sau thành công
```

## Quy tắc chính

- Request dùng `content_format=e2ee_v1`; `content` là chuỗi JSON gồm `version`, `recipient_id`, `sender_identity_key`, `ephemeral_key`, `signed_prekey_id`, `one_time_prekey_id`, `nonce`, `ciphertext`. Public keys, nonce và ciphertext dùng Base64.
- AAD ràng buộc hai IK, UUID thread/message/sender/recipient, EK và prekey IDs; không gồm seq/thời điểm. Kiểu payload: [types.go](../internal/e2ee/types.go); crypto: [crypto.go](../internal/e2ee/crypto.go).
- Mỗi tin mới, kể cả chiều trả lời, chạy lượt X3DH mới. Retry giữ nguyên UUID/body/key; publish lỗi sau commit trả `503`.
- Thiếu private OPK và cached key khi header có OPK ID phải báo lỗi, không chuyển sang 3DH. Giải mã/local commit lỗi không xóa OPK hoặc tăng read marker qua tin lỗi.
- IndexedDB giữ khóa/pending theo API URL/user UUID. Mỗi tài khoản dùng một profile cố định; Web Locks chọn một tab sở hữu E2EE. Server không nhận private key, SK hoặc plaintext E2EE.

Giới hạn bảo mật: [ADR 004](adr/004-e2ee-without-double-ratchet.md). Luồng đọc/realtime: [luồng xử lý](request-flow.md).
