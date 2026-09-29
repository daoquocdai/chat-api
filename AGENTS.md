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
- Thêm test cho hành vi mới và chạy `make test`, `make build` trước khi báo hoàn thành.
- Nếu test cần PostgreSQL hoặc Redis nhưng môi trường chưa sẵn sàng, nói rõ test nào chưa chạy; không ghi là đã đạt.
- Không reset/TRUNCATE database hoặc merge branch nếu chưa được yêu cầu.
- Cuối mỗi lượt, tóm tắt thay đổi, kết quả kiểm tra và giới hạn còn lại.