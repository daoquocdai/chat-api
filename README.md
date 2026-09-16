# Chat API

HTTP server đơn giản viết bằng Go, cung cấp API kiểm tra trạng thái server và gửi, nhận tin nhắn. Tin nhắn hiện được lưu trong RAM.

## API

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/health` | Kiểm tra server |
| `POST` | `/messages` | Tạo tin nhắn |
| `GET` | `/messages` | Lấy danh sách tin nhắn |

## Luồng hoạt động

```text
HTTP request → router → handler → service → repository → HTTP response
```

- Router chuyển request đến handler phù hợp.
- Handler đọc request và trả JSON response.
- Service kiểm tra và xử lý logic.
- Repository lưu hoặc lấy tin nhắn trong RAM.

## Chạy project

```bash
go run ./cmd
```

Server chạy tại `http://localhost:8080`.

Ví dụ tạo tin nhắn:

```bash
curl -X POST http://localhost:8080/messages -H "Content-Type: application/json" -d '{"sender":"alice","receiver":"bob","content":"hello"}'
```

## Chạy test

```bash
go test ./...
```
