# Chat API

Chat API là bản demo nhắn tin 1–1 viết bằng Go, Gin và PostgreSQL. Giao diện dùng HTML/CSS/JavaScript thuần, được Gin phục vụ cùng origin với API nên không cần npm hoặc bước build frontend.

Docker Compose khởi tạo PostgreSQL và Redis. Phiên bản hiện tại lưu user và message trong PostgreSQL; Redis chưa được ứng dụng sử dụng.

## Chức năng hiện có

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/health` | Kiểm tra server |
| `POST` | `/users` | Tạo user |
| `GET` | `/users` | Lấy danh sách user công khai |
| `GET` | `/users/:id` | Lấy user theo `external_id` |
| `POST` | `/messages` | Gửi tin nhắn |
| `GET` | `/messages?user_id=...&peer_id=...` | Lấy lịch sử giữa hai user |
| `GET` | `/` | Mở giao diện demo |

Các trường `id`, `sender_id` và `receiver_id` qua API đều là `external_id` dạng UUID. ID số tự tăng chỉ dùng nội bộ trong PostgreSQL.

Xem [luồng xử lý request](docs/request-flow.md) để hiểu vai trò của router, DTO, handler, service, repository, sqlc và PostgreSQL.

## Chạy từ bản clone mới

Cần cài Go theo phiên bản trong `go.mod`, Docker có Docker Compose và [Goose](https://github.com/pressly/goose). GNU Make là tùy chọn.

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
```

Sao chép config mẫu:

```bash
cp config/config.yml.example config/config.yml
```

Trên Windows CMD:

```bat
copy config\config.yml.example config\config.yml
```

Không đưa `config/config.yml` vào Git. File này đã có trong `.gitignore` và có thể chứa thông tin kết nối cục bộ.

Khởi động database, chạy migration và server:

```bash
go mod download
make up
make migrate
make run
```

Nếu Windows chưa có `make`, chạy trong CMD:

```bat
go mod download
docker compose up -d
set "DATABASE_URL=postgres://chat:chat@localhost:5432/chat_api?sslmode=disable"
goose -dir db/migrations postgres "%DATABASE_URL%" up
go run ./cmd
```

Ứng dụng đọc `database_url` từ `config/config.yml`, còn Goose trong Makefile dùng `DATABASE_URL`. Hai giá trị phải trỏ đến cùng database.

## Demo bằng hai tab

1. Mở `http://localhost:8080` ở hai tab.
2. Tạo hoặc chọn hai user khác nhau.
3. Ở mỗi tab, chọn một user trong **Tôi là** và chọn user còn lại trong **Nhắn cho**.
4. Gửi tin nhắn từ cả hai phía. Giao diện tự lấy lịch sử mỗi giây.
5. Khởi động lại server và chọn lại hai user để thấy lịch sử vẫn còn trong PostgreSQL.

Hiện chưa có đăng nhập hoặc JWT. Người dùng được chọn thủ công trên giao diện để giả lập danh tính cho demo local; client tự gửi `user_id`/`sender_id`, nên không có bảo đảm riêng tư hay xác thực người gửi.

## Giới hạn hiện tại

- Dùng polling mỗi giây, chưa có WebSocket hoặc realtime push.
- Mỗi lần chỉ lấy tối đa 100 tin nhắn gần nhất và chưa có phân trang.
- Chưa có thread/conversation, trạng thái đã đọc hoặc E2EE.
- Redis đang chạy trong Docker Compose nhưng chưa được dùng để phát sự kiện.

## Kiểm tra code

```bash
go test ./...
go vet ./...
go build ./...
```

Các unit test dùng repository và user service giả để kiểm tra nghiệp vụ mà không cần PostgreSQL. Chúng không thay thế kiểm tra migration, SQL và luồng HTTP thật với database.

Khi sửa SQL trong `db/queries`, sinh lại code bằng:

```bash
make sqlc
```

Không sửa trực tiếp các file trong `internal/database/sqlc` vì chúng do sqlc sinh.
