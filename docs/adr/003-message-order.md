# ADR 003: Thứ tự tin theo sequence của thread

## Bối cảnh

Gửi tin đồng thời và event khác thứ tự khiến timestamp không đủ để sắp lịch sử.

## Quyết định

- Khóa thread, kiểm tra membership, tăng `last_seq` và insert trong cùng transaction; text/system dùng chung seq. Retry UUID hợp lệ không tăng seq.
- API trả `seq DESC` với `before_seq`/`next_cursor`; client gộp UUID và hiển thị `seq ASC`.
- Reconnect lấy bù từ trang mới nhất tới `syncedSeq` hoặc hết cursor. Không có `after_seq`; gap thuộc thời gian vắng mặt không được lấy nếu thiếu quyền.

## Đánh đổi

Thao tác trong cùng thread phải tuần tự; lấy bù có thể đọc lại tin đã cache. Không có thứ tự toàn cục giữa các thread. Chi tiết: [luồng xử lý](../request-flow.md).
