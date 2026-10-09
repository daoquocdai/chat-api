# ADR 002: Unread theo membership và read marker

## Bối cảnh

Client có thể chỉ tải một phần lịch sử; unread phải đúng khi event trùng hoặc user tham gia lại nhóm.

## Quyết định

- PostgreSQL COUNT tin text/system do người khác tạo, sau `last_read_seq` trong membership đang hoạt động.
- PUT read giữ khóa thread, dùng `GREATEST` và yêu cầu `joined_seq - 1 <= N <= threads.last_seq`.
- Tham gia lại tạo marker mới. Client chỉ tăng tới seq đã render sau initial sync, khi thread mở, tab hiển thị và ở cuối chat; không vượt `syncedSeq`. Với E2EE, tài khoản phải được mở khóa và tin đã giải mã thành công. Mỗi tab có thể đọc độc lập; marker trên server chỉ tăng.

## Đánh đổi

Không cần counter riêng nhưng COUNT có chi phí truy vấn. Client đối chiếu summary khi cần; unread không bằng hiệu hai seq và marker không chứng minh đã xem từng tin. Chi tiết: [luồng xử lý](../request-flow.md).
