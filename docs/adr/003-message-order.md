# ADR 003: Thứ tự message theo seq của thread

## Bối cảnh

Nhiều người có thể gửi tin/thay đổi thành viên đồng thời. REST phân trang và WebSocket có thể trả tin lặp hoặc khác thứ tự; timestamp không đủ làm thứ tự lịch sử.

## Quyết định

- PostgreSQL cấp `seq` tăng theo thread trong transaction có khóa dòng thread. Kiểm tra membership sau khi lấy khóa, tăng `last_seq`, ghi message và commit cùng nhau. Text/system dùng chung bộ đếm; rollback không giữ lần tăng seq. Retry text hợp lệ bằng `external_id` UUID trả message cũ, không cấp seq lần nữa.
- Lịch sử lấy theo `seq DESC`; `before_seq`/`next_cursor` tải trang cũ hơn. Thứ tự event Redis và timestamp không quyết định thứ tự lịch sử. Client gộp REST/event theo message ID và hiển thị `seq ASC`.
- Client giữ `syncedSeq` local. Sau khi WebSocket reconnect thành công, lấy trang mới nhất rồi lùi bằng `before_seq` tới baseline đã biết hoặc hết lịch sử được phép; gộp các trang vào cache. API hiện **không hỗ trợ `after_seq`**. Request đang chạy được gộp, version phiên/cache ngăn response cập nhật nhầm tài khoản hoặc thread.
- PostgreSQL giới hạn lịch sử theo các khoảng membership. Gap do rời/bị xóa rồi thêm lại không phải tin bị mất; gặp hết cursor thì kết thúc lấy bù, không lặp GET để lấp seq không có quyền xem.

## Đánh đổi

Khóa thread làm thứ tự rõ ràng và đồng bộ được gửi tin với membership/read marker, nhưng các thao tác trong cùng thread phải tuần tự. Mỗi thread có bộ seq riêng, không có thứ tự toàn cục giữa các thread.

Lấy bù bằng trang mới nhất rồi lùi có thể đọc lại tin đã cache và cần nhiều request khi offline lâu. Gộp theo ID tránh bản sao; giữ cách cursor và cache hiện tại để demo dễ giải thích, không thêm endpoint hay migration.
