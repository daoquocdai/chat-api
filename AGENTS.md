# AGENTS.md

## Project
Mini-Hermes là dự án học tập xây hệ thống chat bằng Go.
Đọc README và code liên quan trước khi sửa. Giữ thay đổi đúng phạm vi yêu cầu.

## Architecture
- Luồng phụ thuộc: handler → service → repository; handler không gọi repository trực tiếp.
- Service không phụ thuộc Gin; repository không xử lý JWT.
- PostgreSQL là nguồn dữ liệu message và thứ tự `seq` theo thread.
- `messages.external_id` là UUID client dùng để retry; không tạo message hoặc tăng `seq` lần nữa khi retry hợp lệ.

## Workflow
- Dùng các lệnh trong Makefile khi phù hợp.
- Chỉ duy trì test service tại `internal/module/user/service/service_test.go`, `internal/module/thread/service/service_test.go`, `internal/module/message/service/service_test.go` và `internal/wsticket/service_test.go`; ngoại lệ E2EE theo Prompt 1 là `internal/e2ee/crypto_test.go` và `internal/module/e2ee/service/service_test.go`. Không thêm test thường trực ngoài sáu file này.
- Với web/gateway, kiểm chứng thủ công hoặc bằng script tạm rồi xóa script tạm. Chạy `make test`, `make build` khi phù hợp; nếu không chạy được thì dùng lệnh Go tương đương và nêu rõ giới hạn kiểm chứng.
- Không reset/TRUNCATE database hoặc merge branch nếu chưa được yêu cầu.
- Cuối mỗi lượt, tóm tắt thay đổi, kết quả kiểm tra và giới hạn còn lại.
