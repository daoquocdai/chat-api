# Luồng E2EE có thể khôi phục

Direct dùng E2EE; nhóm dùng plaintext. Crypto chạy trong Go/WASM, còn JavaScript gọi API và cập nhật giao diện. X3DH chạy khi tạo **phiên khóa (epoch)**, không chạy lại cho từng tin. Không dùng Double Ratchet.

## Đăng ký

```mermaid
sequenceDiagram
    participant U as Người dùng
    participant C as Client / WASM
    participant A as HTTP API
    participant D as PostgreSQL
    U->>C: Username + mật khẩu
    C->>C: Salt ngẫu nhiên, Argon2id rồi HKDF
    Note over C: Hai giá trị riêng: auth_credential và vault_key
    C->>C: Sinh IK, SPK và 20 OPK; mã hóa private keys
    C->>A: POST /auth/register: username, auth_credential, kdf, public_bundle, account_vault
    A->>D: Transaction: hash credential + account + public OPK
    A-->>C: Tài khoản đã tạo
    C->>A: POST /auth/login: username + auth_credential
    A-->>C: JWT + user_id
    C->>A: GET /e2ee/account với JWT
    A-->>C: KDF + public bundle gốc + vault mã hóa
    C->>C: Mở vault bằng vault_key, xác minh bộ khóa
```

Mật khẩu gốc và `vault_key` không gửi tới backend. `auth_credential` là Base64 của 32 byte; server bcrypt chuỗi này. KDF cố định: Argon2id, salt 16 byte, 65536 KiB, 3 lượt, parallelism 4; sau đó HKDF tách credential và khóa bảo vệ vault.

Vault chứa private IK/SPK và cả 20 private OPK. Public bundle gốc được giữ nguyên để xác thực vault, kể cả khi public OPK đã bị claim khỏi hàng đợi `prekeys`. Không có nút upload/refill prekey thủ công.

## Đăng nhập trên máy hoặc tab mới

1. `GET /auth/params?username=...` lấy username chuẩn hóa và KDF công khai.
2. WASM dùng mật khẩu và đúng KDF để tạo lại `auth_credential` và `vault_key`.
3. `POST /auth/login` gửi username/credential, nhận JWT.
4. `GET /e2ee/account` lấy vault mã hóa; client mở và kiểm tra private/public keys.
5. Khi mở direct, lấy lịch sử và `GET /threads/:id/epochs`. Khôi phục khóa của đúng epoch để giải mã từng tin, gồm cả tin đã gửi.

Mỗi thiết bị làm độc lập; không cần tab cũ hay IndexedDB. Reload giữ JWT và khóa mở vault trong `sessionStorage` của tab, rồi tải lại account từ API. Phiên ẩn danh mới chỉ cần đăng nhập lại bằng mật khẩu.

## Tạo phiên khóa lần đầu

```mermaid
sequenceDiagram
    participant S as Client gửi
    participant A as API / PostgreSQL
    participant R as Client nhận
    S->>A: GET /threads/:id/epochs
    A-->>S: Chưa có current_epoch_id
    S->>A: POST /e2ee/bundles/:peer_id/claim với thread_id
    A-->>S: Public IK/SPK + một OPK nếu còn
    S->>S: Xác minh SPK; X3DH 3/4 DH tạo SK và bootstrap
    S->>S: Mã hóa backup SK bằng khóa dẫn xuất từ vault_key
    S->>A: POST /threads/:id/epochs: epoch_id, previous_epoch_id=null, bootstrap, key_backup
    A->>A: Khóa thread, tạo epoch + backup sender + cập nhật current trong một transaction
    A-->>S: Epoch đã lưu (201); retry đúng trả cùng epoch (200)
    R->>A: GET /threads/:id/epochs
    A-->>R: Bootstrap và key_backup riêng của recipient (có thể null)
    R->>R: Nếu thiếu backup: khôi phục private prekeys, tính SK, xác minh confirmation
    R->>A: PUT /threads/:id/epochs/:epoch_id/key với backup SK đã mã hóa
    A-->>R: Backup canonical đã lưu
    R->>R: Mở backup, kiểm tra cùng SK rồi chấp nhận phiên
```

Nếu hai client cùng đề xuất epoch, `previous_epoch_id` và khóa thread chọn một phiên. Bên thua nhận `409` kèm epoch thắng, khôi phục phiên đó và bỏ SK đề xuất. Khi public OPK hết, X3DH dùng 3 DH; private OPK cũ vẫn nằm trong vault mã hóa để khôi phục các epoch trước.

Mỗi user chỉ tải được backup SK của mình. Backup đầu tiên bất biến; gửi lại không ghi đè. Nó được xác thực với owner UUID, thread UUID, epoch UUID và digest của bootstrap. Sender chỉ gửi tin sau khi epoch và backup đã được server xác nhận.

## Gửi, nhận và đọc lịch sử

1. Client lấy epoch hiện tại hoặc khôi phục epoch đã có; không claim bundle cho mỗi tin.
2. Sinh message UUID. HKDF dẫn xuất khóa tin từ SK với context gồm thread, epoch, message UUID, sender và recipient; hai chiều dùng context khác nhau.
3. AES-GCM mã hóa nội dung. Gửi `POST /threads/:id/messages` với `message_id`, `content_format: "e2ee_v2"` và `content` là chuỗi JSON:

```json
{"version":2,"epoch_id":"UUID","recipient_id":"UUID","nonce":"Base64","ciphertext":"Base64"}
```

4. Backend kiểm tra membership, người nhận và epoch thuộc thread, rồi lưu bản mã cùng seq. Response chứa đầy đủ message đã lưu.
5. Redis/gateway phát sự kiện tới mọi kết nối của **cả sender lẫn recipient**. Client gộp response HTTP và WebSocket theo message UUID.
6. Client khôi phục SK của epoch trong envelope, dẫn xuất lại khóa tin và giải mã. `recipient_id` trên frame WebSocket là đích giao sự kiện; khác với người nhận mật mã trong envelope.

Retry giữ nguyên UUID, epoch và toàn bộ bản mã; không chạy X3DH hay mã hóa lại. Ciphertext pending được lưu theo UUID trong `localStorage`, hoặc RAM khi storage không dùng được. Outbox này không phải nguồn phục hồi khóa.

Nút **Đổi phiên khóa** tạo epoch mới với `previous_epoch_id` hiện tại. Epoch và backup cũ vẫn được giữ để đọc lịch sử. Đăng nhập/reconnect/đổi máy không tự đổi epoch.

## Giới hạn

Khôi phục cần đúng mật khẩu và dữ liệu vault/epoch còn trên server. Chưa có đổi mật khẩu hay cơ chế khôi phục khi quên mật khẩu. Việc giữ private prekeys và SK mã hóa phục vụ đọc lịch sử, giảm lợi ích của việc xóa khóa; thiết kế không có bảo vệ phục hồi sau xâm nhập của Double Ratchet. Dùng HTTPS/WSS và frontend đáng tin cậy; đây là profile của dự án, không phải triển khai Signal hoàn chỉnh hay giao thức đã được audit.

Code chính: [account.go](../internal/e2ee/account.go), [epoch.go](../internal/e2ee/epoch.go), [messages.go](../internal/e2ee/messages.go), [client](../web/e2ee.js), [repository](../internal/module/e2ee/repository/postgres.go).
