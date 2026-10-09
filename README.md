# Mini-Hermes Chat API

Ứng dụng chat dùng Go, Gin, PostgreSQL, sqlc và Redis. Direct luôn E2EE; nhóm dùng plaintext. Go/WASM mã hóa tại client, HTTP API lưu tin và gateway riêng phát WebSocket.

Một mật khẩu tài khoản đủ để khôi phục khóa và lịch sử trên thiết bị hoặc phiên ẩn danh mới. Nhiều tab/thiết bị dùng cùng tài khoản đồng thời. Mỗi direct khởi tạo X3DH một lần rồi dùng lại cùng phiên khóa; mỗi tin chỉ dẫn xuất khóa bằng HKDF, không dùng Double Ratchet.

## Chạy local

Yêu cầu: Go theo [go.mod](go.mod), Docker Compose, `goose`, `make`. Trình duyệt cần secure context, WebAssembly và `crypto.randomUUID`; localhost đáp ứng secure context. Không cần IndexedDB hoặc Web Locks.

```powershell
Copy-Item config/config.yml.example config/config.yml
# Thay auth.jwt_secret trong config/config.yml trước khi chạy.
docker compose up -d
$env:DATABASE_URL = 'postgres://chat:chat@localhost:5432/chat_api?sslmode=disable'
goose -dir db/migrations postgres $env:DATABASE_URL up
make wasm
```

Chạy `go run ./cmd` và `go run ./cmd/ws-gateway` ở hai terminal; mở `http://localhost:8080`. Đăng ký tự chuẩn bị và sao lưu khóa E2EE, không cần upload bundle thủ công. Mở tab/profile khác rồi đăng nhập bằng cùng username/mật khẩu để dùng nhiều thiết bị.

API/gateway đọc `config/config.yml`; `DATABASE_URL` ở trên dùng cho migration. Khi đổi host, cập nhật `ws_public_url`, `ws_allowed_origins` và dùng HTTPS/WSS. Binary WASM và `wasm_exec.js` phải được build từ cùng Go toolchain.

### Reset dữ liệu local

Migration `20261009100000` yêu cầu database phát triển đã reset: tài khoản cũ không có encrypted vault và không được chuyển tự động. Sau reset phải đăng ký lại; vault/public prekeys mới được tạo tự động. Migration `down` không phục hồi dữ liệu đã xóa.

Dừng API/gateway. Với schema hiện tại của database `chat_api` trong Docker Compose dự án, xóa cả bảy bảng nghiệp vụ:

```powershell
docker compose exec -T postgres psql -U chat -d chat_api -v ON_ERROR_STOP=1 -c "TRUNCATE TABLE messages, participants, threads, prekeys, users, e2ee_epochs, e2ee_epoch_backups RESTART IDENTITY CASCADE;"
goose -dir db/migrations postgres $env:DATABASE_URL up
```

Nếu nâng từ schema cũ chưa có hai bảng epoch, truncate năm bảng cũ (`messages`, `participants`, `threads`, `prekeys`, `users`) với `CASCADE` trước khi chạy migration mới. Trong Redis database được cấu hình, xóa Stream ở `redis.stream`, cache `<stream>:membership:*` và vé `mini-hermes:ws-ticket:*` của dự án; giữ nguyên key ngoài các namespace này. Gateway tạo lại consumer group sau khi Stream bị xóa.

Đăng xuất rồi đăng ký/đăng nhập lại trong browser. Reset server không xóa bộ nhớ browser; outbox cũ được scope theo origin/API và user UUID, không dùng cho UUID tài khoản mới.

## HTTP API

Auth API không nhận mật khẩu gốc. Browser dẫn xuất `auth_credential` và vault key riêng; chỉ credential đi tới server. Các endpoint yêu cầu `Authorization: Bearer <access_token>`, trừ health và ba auth endpoints công khai.

| Method | Path | Chức năng / request chính |
| --- | --- | --- |
| `GET` | `/health` | Trạng thái API |
| `GET` | `/auth/params?username=...` | Username chuẩn hóa và KDF công khai |
| `POST` | `/auth/register` | `username`, `auth_credential`, `kdf`, `public_bundle`, `account_vault` |
| `POST` | `/auth/login` | `username`, `auth_credential`; trả JWT, `token_type`, `user_id` |
| `GET` | `/users` | Danh sách tài khoản |
| `POST` | `/auth/ws-ticket` | Vé WebSocket một lần |
| `GET` | `/e2ee/account` | Vault và public bundle gốc của actor |
| `POST` | `/threads/direct` | Tạo/mở direct bằng `peer_id` |
| `POST` | `/threads/group` | Tạo nhóm bằng `name`, `member_ids` |
| `GET` | `/threads` | Thread đang tham gia |
| `GET` | `/threads/:id/members` | Thành viên nhóm |
| `POST` | `/threads/:id/members` | Admin thêm `user_id` |
| `DELETE` | `/threads/:id/members/:user_id` | Admin xóa người khác |
| `POST` | `/threads/:id/leave` | Rời nhóm |
| `POST` | `/e2ee/bundles/:user_id/claim` | Bundle peer khi khởi tạo phiên direct; body `thread_id` |
| `GET` | `/threads/:id/epochs` | Bootstrap, backup của actor và `current_epoch_id` |
| `POST` | `/threads/:id/epochs` | Khởi tạo phiên cố định: `epoch_id`, `bootstrap`, `key_backup` |
| `PUT` | `/threads/:id/epochs/:epoch_id/key` | Lưu `key_backup`; trả bản đã commit |
| `GET` | `/threads/:id/messages` | Lịch sử với `before_seq`, `limit` |
| `POST` | `/threads/:id/messages` | `message_id`, `content_format`, `content` |
| `PUT` | `/threads/:id/read` | Tăng `last_read_seq` |

Actor lấy từ JWT `sub`. Gateway nhận `GET /ws?ticket=...`; ticket được tiêu thụ một lần trước upgrade. Claim/epoch APIs chỉ áp dụng cho direct, không cho nhóm. Không còn endpoint upload/refill prekeys.

## Khóa, epoch và khôi phục

- **Một mật khẩu:** Argon2id profile v1 cố định: salt 16 byte, `memory_kib=65536`, `iterations=3`, `parallelism=4`, output 32 byte. HKDF-SHA-256 tách auth credential/vault key theo mục đích và username. Server lưu bcrypt hash của credential, không nhận mật khẩu gốc hoặc vault key.
- **Vault bất biến:** đăng ký sinh IK, SPK và 20 OPK; hash, KDF, public bundle gốc, encrypted vault và public OPK được commit cùng transaction. Claim xóa public OPK khả dụng, nhưng bundle gốc và private OPK trong encrypted vault vẫn giữ để phục hồi. Không có luồng đổi mật khẩu.
- **Phiên cố định:** mỗi direct khởi tạo X3DH 3/4 DH một lần, tạo bootstrap có key confirmation và backup SK mã hóa. Khóa thread chọn phiên đầu tiên; epoch, backup sender và con trỏ current commit cùng transaction. Đề xuất khác trả `409` kèm phiên canonical để client khôi phục. Recipient xác minh bootstrap rồi lưu backup riêng và chờ ACK trước khi dùng SK. Hết public OPK, direct mới dùng 3DH.
- **Tin direct:** `content_format=e2ee_v2`; `content` là chuỗi JSON gồm `version`, `epoch_id`, `recipient_id`, `nonce`, `ciphertext`. Mỗi tin dẫn xuất key từ SK và context thread/epoch/message/sender/recipient, rồi AES-256-GCM. Reply dùng cùng epoch, không claim/X3DH lại.
- **Retry:** giữ nguyên UUID, epoch và envelope; trả cùng `seq`. UUID cũ khác payload trả `409`. Mọi thiết bị và mọi tin của direct dùng lại phiên đã khởi tạo; đăng nhập, reload và reconnect chỉ khôi phục khóa, không đổi phiên.

ID epoch, các bảng epoch/backup và API danh sách vẫn được giữ để đọc lịch sử tương thích đã có. Giao diện và API khởi tạo không hỗ trợ thay phiên đang dùng.

Account keys/SK ở RAM sau khi mở vault. `sessionStorage` giữ JWT, username, user UUID và `vault_key` cho reload cùng phiên; đăng xuất xóa chúng. `localStorage` chỉ giữ outbox theo UUID với context/ciphertext/body gửi lại, không lưu plaintext/private keys/SK. Nếu storage không khả dụng, outbox ở RAM; lịch sử đã commit vẫn phục hồi từ server khi đăng nhập lại.

Đối chiếu UUID/fingerprint của peer qua kênh đã xác thực. Quên mật khẩu và mất mọi khóa đã mở thì không thể phục hồi lịch sử. Archive khóa phục vụ khôi phục làm giảm bảo vệ từ việc xóa khóa; đây không phải Double Ratchet hay giao thức đã được audit.

## Nhóm, lịch sử và realtime

- Nhóm dùng `plaintext`, tin dài 1–1000 ký tự Unicode, tối đa 100 thành viên. Admin thêm/xóa người khác; mọi thành viên được rời. Lịch sử chỉ thuộc các khoảng membership. Tạo nhóm chưa có idempotency key; lỗi sau commit phải đối chiếu trước khi tạo lại.
- Lịch sử mặc định 30 tin, tối đa 100, trả `seq DESC` và `next_cursor`; client hiển thị tăng dần. Unread đếm tin của người khác sau read marker trong membership hiện tại; marker chỉ tăng.
- Event gửi tới toàn bộ snapshot membership, **kể cả sender**, để các thiết bị của sender thấy tin mới. `recipient_id` trên WS là user nhận delivery; trong encrypted envelope là peer mật mã. Client gộp UUID và lấy bù REST.
- Publish lỗi sau DB commit trả `503`; event có thể trùng. Chưa có transactional outbox ở backend. `XACK` không xác nhận đã giao/đọc; gateway hiện hỗ trợ một instance.

## Kiểm tra

Build WASM trước kiểm thử client; Node cần hỗ trợ `fetch` và WebSocket.

```powershell
go test ./...
go build ./...
go vet ./...
node --test web/e2ee-client.test.cjs
```

Kiểm thử client dùng Go/WASM thật với API fixture; phần HTTP thật được bật riêng. Sau migration, chạy trên PostgreSQL/Redis local của dự án:

```powershell
$env:CHAT_API_INTEGRATION = '1'
go test ./internal/route -count=1
Remove-Item Env:CHAT_API_INTEGRATION

# API và gateway phải đang chạy cho bước này.
$env:CHAT_API_TEST_URL = 'http://localhost:8080'
node --test web/e2ee-client.test.cjs
Remove-Item Env:CHAT_API_TEST_URL
```

Các bài tích hợp tạo fixture trong database phát triển: auth/vault, khởi tạo phiên đồng thời và dùng lại phiên cố định, từ chối đề xuất thay thế bằng `409`, nhiều thiết bị, recipient offline, khôi phục lịch sử từ client trống, retry/ACK, Redis và bản sao sender qua WS. Có thể [reset](#reset-dữ-liệu-local) sau kiểm tra.

## Tài liệu

- [ERD](docs/chat-api-erd.md), [luồng xử lý](docs/request-flow.md), [luồng E2EE](docs/e2ee-sequence.md), [thiết kế phục hồi](docs/e2ee-next-design.md).
- [ADR 001: gateway](docs/adr/001-gateway-and-stream.md), [ADR 002: unread](docs/adr/002-unread-count.md), [ADR 003: thứ tự tin](docs/adr/003-message-order.md), [ADR 004: thiết kế E2EE cũ](docs/adr/004-e2ee-without-double-ratchet.md), [ADR 005: E2EE có thể khôi phục](docs/adr/005-recoverable-e2ee-sessions.md).
