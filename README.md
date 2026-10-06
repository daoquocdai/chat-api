# Mini-Hermes Chat API

Ứng dụng chat dùng Go, Gin, PostgreSQL, sqlc và Redis. Hỗ trợ direct plaintext/E2EE và nhóm plaintext. HTTP API lưu tin; gateway riêng phát WebSocket. E2EE chạy tại client bằng Go/WASM.

## Chạy local

Yêu cầu: Go theo [go.mod](go.mod), Docker Compose, `goose` và `make`. E2EE cần trình duyệt hỗ trợ secure context, WASM, IndexedDB và Web Locks.

Sao chép cấu hình, thay `auth.jwt_secret` và kiểm tra `database_url`, Redis, origin WebSocket. Sau đó chạy migration và build WASM:

```powershell
Copy-Item config/config.yml.example config/config.yml
# Thay jwt_secret trong config/config.yml trước khi chạy ứng dụng.
docker compose up -d
$env:DATABASE_URL = 'postgres://chat:chat@localhost:5432/chat_api?sslmode=disable'
goose -dir db/migrations postgres $env:DATABASE_URL up
make wasm
```

Chạy `go run ./cmd` và `go run ./cmd/ws-gateway` ở hai terminal; mở `http://localhost:8080`.

API/gateway đọc `config/config.yml`; `DATABASE_URL` ở trên chỉ dùng cho migration. Khi đổi host, cập nhật `ws_public_url`, `ws_allowed_origins` và dùng HTTPS/WSS. WASM và `wasm_exec.js` phải được build từ cùng Go toolchain.

Migration `20260924090000` thay bảng messages legacy và không hỗ trợ user cũ thiếu password hash. Chỉ chạy trên database thử nghiệm đã chuẩn bị; migration `down` không khôi phục dữ liệu bị xóa.

## HTTP API

`/health`, `/auth/register`, `/auth/login` công khai. Các endpoint còn lại yêu cầu `Authorization: Bearer <access_token>`; actor lấy từ JWT `sub`.

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/health` | Trạng thái API |
| `POST` | `/auth/register` | Đăng ký bằng username/password |
| `POST` | `/auth/login` | Nhận JWT |
| `GET` | `/users` | Danh sách tài khoản |
| `POST` | `/auth/ws-ticket` | Vé WebSocket một lần |
| `POST` | `/threads/direct` | Tạo/mở direct bằng `peer_id`, tùy chọn `encryption_mode` |
| `POST` | `/threads/group` | Tạo nhóm bằng `name`, `member_ids` |
| `GET` | `/threads` | Thread đang tham gia |
| `GET` | `/threads/:id/members` | Thành viên nhóm |
| `POST` | `/threads/:id/members` | Admin thêm thành viên |
| `DELETE` | `/threads/:id/members/:user_id` | Admin xóa thành viên |
| `POST` | `/threads/:id/leave` | Rời nhóm |
| `GET` | `/threads/:id/messages` | Lịch sử với `before_seq`, `limit` |
| `POST` | `/threads/:id/messages` | Gửi `message_id`, `content_format`, `content` |
| `PUT` | `/threads/:id/read` | Tăng `last_read_seq` |
| `POST` | `/e2ee/prekeys` | Đăng ký/bổ sung public prekeys |
| `POST` | `/e2ee/bundles/:user_id/claim` | Lấy bundle peer với body `thread_id` |

## Quy tắc chính

- **Direct:** một thread cho mỗi cặp user; mặc định plaintext. E2EE mới yêu cầu hai bên đã đăng ký IK/SPK. Yêu cầu mode khác thread hiện tại trả `409`.
- **Gửi/retry:** client sinh UUID cho mỗi tin; retry giữ UUID/payload, trả cùng `seq`. Payload khác với UUID cũ trả `409`. Publish lỗi sau commit trả `503`; tin vẫn đã lưu.
- **Nội dung:** plaintext dài 1–1000 ký tự Unicode. E2EE dùng `content_format=e2ee_v1`; `content` là chuỗi JSON envelope chứa header, nonce và ciphertext.
- **Nhóm:** tối đa 100 thành viên. Admin thêm/xóa người khác; mọi thành viên được rời. Lịch sử chỉ nằm trong các khoảng membership, không gồm thời gian vắng mặt. Tạo nhóm chưa có idempotency key; lỗi sau commit phải đối chiếu trước khi tạo lại.
- **Lịch sử/đọc:** mặc định 30 tin, tối đa 100, trả `seq DESC` và `next_cursor`; client hiển thị `seq ASC`. Unread đếm tin text/system do người khác tạo sau read marker trong lần tham gia hiện tại.
- **Realtime:** PostgreSQL là nguồn lịch sử. Event có thể trùng; client gộp UUID và lấy bù REST khi reconnect hoặc có gap. `XACK` không xác nhận người dùng đã đọc.

## Sử dụng E2EE

1. Dùng hai profile riêng, mỗi tài khoản một tab sở hữu E2EE.
2. Chọn **Khởi tạo E2EE cho tài khoản mới** rồi **Đăng ký public bundle** ở cả hai bên.
3. Chọn **E2EE** khi mở direct mới; đối chiếu UUID/fingerprint qua kênh đã xác thực.
4. Gửi lỗi dùng **Gửi lại pending**; reload giữ khóa/pending trong IndexedDB. Bổ sung khóa bằng **Bổ sung 20 OPK**.

Mỗi tin mới chạy X3DH riêng. IK/SPK bất biến; mất state không khôi phục được khóa cũ. Chưa có Double Ratchet, multi-device, backup/khôi phục khóa hoặc E2EE nhóm. Gateway hiện hỗ trợ một instance, chưa có outbox.

## Kiểm tra

```powershell
go test ./...
go build ./...
go vet ./...
Get-ChildItem web/*.js | ForEach-Object { node --check $_.FullName }
```

Kiểm thử tích hợp cần PostgreSQL/Redis và trình duyệt: gửi/retry, offline hơn 30 tin, read marker, rời/tham gia lại nhóm, E2EE hai chiều và reload.

## Tài liệu

- [ERD](docs/chat-api-erd.md), [luồng xử lý](docs/request-flow.md), [luồng X3DH](docs/e2ee-sequence.md).
- [ADR 001: gateway/stream](docs/adr/001-gateway-and-stream.md), [ADR 002: unread](docs/adr/002-unread-count.md), [ADR 003: thứ tự tin](docs/adr/003-message-order.md), [ADR 004: E2EE](docs/adr/004-e2ee-without-double-ratchet.md).
