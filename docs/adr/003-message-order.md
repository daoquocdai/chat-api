# ADR #3 — Thứ tự message do PostgreSQL seq quyết định

Trạng thái: chọn cho demo tuần 4.

## Bối cảnh

Nhiều người có thể gửi đồng thời; hệ thống còn ghi tin tạo nhóm, thêm/xóa/rời thành viên. XADD xảy ra sau commit nên thứ tự vào Redis hoặc tới WebSocket có thể khác thứ tự lưu DB.

## Quyết định

Mọi thao tác ghi message trong một thread khóa cùng dòng `threads`, tăng last_seq và insert message trong cùng transaction. Thay đổi membership dùng mốc seq của system message. Read marker cũng khóa thread để không chạy qua một thay đổi membership đang dở.

Sequence chỉ có ý nghĩa trong một thread. Client gộp theo message UUID, sắp theo seq và tải lịch sử theo cursor before_seq. Stream ID chỉ định danh entry Redis, không quyết định thứ tự chat. Retry UUID/payload hợp lệ trả message cũ, không tăng seq.

## Đánh đổi

Các ghi trên cùng thread tuần tự; các thread khác vẫn chạy song song. Thread rất nóng sẽ bị giới hạn bởi khóa này. Gateway crash trước XACK hoặc API retry sau timeout có thể phát lại event; đây không phải exactly-once. User rời rồi được thêm lại có khoảng seq không được xem, nên client nhóm không được coi mọi gap là mất tin và retry vô hạn.
