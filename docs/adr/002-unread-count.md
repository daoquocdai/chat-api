# ADR #2 — Tính unread từ message và read marker

Trạng thái: chọn cho demo tuần 4.

## Bối cảnh

Mỗi user đọc cùng một thread ở thời điểm khác nhau. Tin mình gửi và system message không tính unread. User được thêm lại cũng không được tính tin gửi trong lúc vắng mặt, nên `last_seq - last_read_seq` không đúng.

## Quyết định

Lưu `last_read_seq` trong từng khoảng membership của `participants`. Marker chỉ tăng bằng `GREATEST` và chỉ được cập nhật khi đang tham gia, không vượt `threads.last_seq`.

`GET /threads` tính `COUNT(*)` các message có seq từ joined_seq của khoảng hiện tại, lớn hơn marker, sender khác user và kind khác system. Không lưu thêm unread_count hoặc is_read trên từng message. Client có thể cập nhật số hiển thị từ event; PostgreSQL summary là nguồn đối chiếu sau reconnect.

## Đánh đổi

Ít trạng thái phải đồng bộ, dễ kiểm tra quyền và tránh lệch bộ đếm. Đổi lại summary có chi phí COUNT theo từng thread; ở quy mô lớn cần đo query plan/index trước khi cân nhắc bộ đếm lưu sẵn. Không coi Redis pending hoặc XACK là trạng thái đã đọc.
