# Mini-Hermes Chat API

Mini-Hermes là demo chat 1-1 dùng Go, Gin, PostgreSQL và sqlc. Người dùng đăng ký bằng mật khẩu, đăng nhập nhận JWT, chọn một tài khoản khác và trao đổi tin nhắn được lưu trong PostgreSQL.

ERD 5 bảng vẫn đang chờ mentor duyệt. Migration mới trên nhánh `feat/auth` là bản thử nghiệm để review, chưa nên áp dụng cho môi trường dùng chung hoặc production.

## API

Public:

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/health` | Kiểm tra server |
| `POST` | `/auth/register` | Đăng ký với `username`, `password` |
| `POST` | `/auth/login` | Nhận JWT access token |

Yêu cầu `Authorization: Bearer <access_token>`:

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/users` | Danh sách tài khoản trong PostgreSQL |
| `POST` | `/threads/direct` | Tạo hoặc mở direct thread với `peer_id` |
| `GET` | `/threads` | Danh sách direct thread của người gọi |
| `GET` | `/threads/:id/messages` | Một trang lịch sử theo cursor `before_seq`, `limit` |
| `POST` | `/threads/:id/messages` | Gửi tin plaintext |
| `PUT` | `/threads/:id/read` | Tăng read marker bằng `last_read_seq` |

JWT chứa external user UUID trong `sub`. Client không được gửi `sender_id` hoặc `user_id` để chọn danh tính. Service tra user nội bộ từ JWT và repository chỉ đọc/ghi khi user là participant đang hoạt động của thread.

Gửi tin nhận `client_msg_id` UUID và `content`. Retry cùng `client_msg_id` bởi cùng người gửi trong cùng thread trả lại tin đã lưu, không tăng `seq` lần nữa.

`GET /threads/:id/messages` mặc định lấy 30 tin mới nhất, tối đa 100. API trả message theo `seq DESC` trong `{ "messages": [...], "next_cursor": ... }`; truyền `before_seq=<next_cursor>` để lấy trang cũ hơn. Danh sách thread và response tạo/mở direct thread có `last_read_seq`, `peer_last_read_seq` và `unread_count`. `unread_count` chỉ đếm message thực tế do người khác gửi, không tính system message.

`PUT /threads/:id/read` chỉ nhận `last_read_seq`; danh tính luôn đến từ JWT. Marker chỉ tăng, phải nằm trong phạm vi participant được xem và không vượt `threads.last_seq`.

Phạm vi hiện tại không có group chat, E2EE, WebSocket hay Redis.

## Chuẩn bị database local

Sao chép cấu hình mẫu và thay JWT secret:

```powershell
Copy-Item config/config.yml.example config/config.yml
```

`config/config.yml` cần trỏ đến PostgreSQL local và có cấu hình tương tự:

```yaml
auth:
  jwt_secret: "replace-with-a-long-random-secret"
  jwt_ttl: 24h
```

Runtime từ chối khởi động nếu secret trống hoặc TTL nhỏ hơn một giây.

### Cảnh báo dữ liệu legacy

Migration mới là `db/migrations/20260924090000_create_direct_chat_schema.sql`. Nó thêm `users.password_hash NOT NULL`, sau đó thay bảng `messages` sender/receiver cũ bằng schema theo thread.

Câu `ALTER TABLE users ... password_hash NOT NULL` đứng đầu migration và không có default. Nếu còn user legacy, migration sẽ thất bại trước khi `DROP TABLE messages`; không có mật khẩu giả được tạo.

Chỉ khi chấp nhận bỏ dữ liệu trên database local thử nghiệm, chạy tường minh:

```powershell
docker compose up -d
docker compose exec postgres psql -U chat -d chat_api -c "TRUNCATE TABLE messages, users RESTART IDENTITY CASCADE;"
$env:DATABASE_URL = 'postgres://chat:chat@localhost:5432/chat_api?sslmode=disable'
goose -dir db/migrations postgres $env:DATABASE_URL up
```

Không chạy lệnh `TRUNCATE` trên database dùng chung, staging hoặc production. `goose down` chỉ phục hồi hình dạng schema cũ, không phục hồi row đã xóa hoặc message cũ đã bị thay thế.

Migration giữ năm bảng trong ERD và thêm các invariant cần cho demo:

- Cặp direct user được sắp `low < high` và có unique index, ngăn hai thread cho cùng cặp khi tạo đồng thời.
- Thread và hai participant được ghi trong cùng transaction.
- `threads.last_seq` được khóa, tăng và ghi message trong cùng transaction.
- `(thread_id, sender_id, client_msg_id)` là ranh giới idempotency.
- `prekeys` có schema để mentor review nhưng chưa có API hoặc nghiệp vụ E2EE.
- `participants.last_read_seq` lưu một read marker tăng đơn điệu; không tạo `is_read` trên từng message.

## Chạy thử thủ công

Sau khi migration thành công:

```powershell
go run ./cmd
```

Mở `http://localhost:8080`:

1. Đăng ký Alice và Bob.
2. Đăng nhập Alice, chọn Bob và gửi tin.
3. Mở một tab độc lập hoặc cửa sổ riêng tư, đăng nhập Bob, chọn Alice và trả lời.
4. Tải lại trang hoặc khởi động lại server; đăng nhập và chọn lại peer để xem lịch sử còn trong PostgreSQL.
5. Đăng nhập tài khoản thứ ba để xác nhận tài khoản đó không thể truy cập thread Alice–Bob bằng API.

Web lưu JWT trong `sessionStorage` của từng tab, gửi Bearer token cho mọi API user/thread/message và polling mỗi 1,5 giây. Trang mới nhất được hợp nhất theo `seq`, các trang cũ đã tải được giữ lại và nút **Tin cũ hơn** dùng `next_cursor`. Nếu hơn một trang tin đến giữa hai lần poll, client tiếp tục đi ngược cursor đến message mới nhất đã biết để không tạo khoảng trống. Web chỉ gửi read marker khi cuộc chat đang mở, tab đang hiển thị và các tin nhận liên tiếp đã thực sự xuất hiện trong viewport. Reload cùng tab vẫn giữ phiên; đăng xuất hoặc đóng tab sẽ xóa phiên local.

Collection [docs/week2-chat.http](docs/week2-chat.http) minh họa đầy đủ hai người chat, request thiếu JWT và cả thao tác đọc/gửi bị từ chối với tài khoản thứ ba. Đổi biến `@run`, rồi chạy request từ trên xuống dưới.

Xem [docs/request-flow.md](docs/request-flow.md) để biết ranh giới handler/service/repository và transaction.

## Chưa có trong scope

- Group chat, E2EE và API prekey.
- WebSocket/realtime push; web đang polling.
- Redis.
- Test table-driven cho service message/thread sau lần bổ sung cursor và read state.
