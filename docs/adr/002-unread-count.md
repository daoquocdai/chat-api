# ADR 002: Unread theo membership và read marker

## Bối cảnh

Direct/group cần badge unread đúng khi nhận event lặp, đọc ở tab khác, rời nhóm hoặc được thêm lại. Client có thể chỉ tải một phần lịch sử.

## Quyết định

- PostgreSQL tính unread bằng `COUNT(*)`, không lưu bộ đếm riêng. Summary chỉ dùng membership đang active: message có `seq >= joined_seq`, `seq > last_read_seq` và sender khác người gọi. Cả text lẫn system đều được tính; thao tác do chính mình tạo không tăng unread.
- Read marker thuộc participant và chỉ tăng bằng `GREATEST`. PUT cần membership active, mốc không trước `joined_seq - 1` và không vượt `threads.last_seq`; transaction giữ khóa thread.
- Thêm lại nhóm tạo khoảng membership/read marker mới, bắt đầu trước system message thêm người. Thông báo thêm tính unread nếu do người khác tạo. Lịch sử vẫn xem được trong hợp các khoảng từng tham gia; không được xem khoảng vắng mặt, không mang unread cũ sang lần tham gia mới.
- Web cập nhật badge từ event/response, khử event trùng và đối chiếu summary khi còn unread cần đếm lại, thiếu thông tin, server trả marker cao hơn mốc gửi, membership thay đổi hoặc reconnect. Nếu PUT đã phủ last seq đang biết và xác định unread bằng 0, không GET chỉ vì trước PUT có unread. Các lần đối chiếu đang chạy được gộp; response cũ bị bỏ theo phiên, membership và thay đổi read marker.
- Chỉ đánh dấu tới **tin cuối đã render và hiển thị** của thread active khi tab visible và người dùng ở cuối chat. Không dùng seq chưa render từ summary. Với 45 unread, trang đầu 30 tin mới nhất có thể đưa marker tới tin cuối và unread về 0, dù 15 tin trước chưa tải.
- `XACK` chỉ thuộc xử lý Redis Stream, độc lập với read marker và unread.

## Đánh đổi

COUNT trong PostgreSQL là nguồn đối chiếu rõ ràng, không cần đồng bộ một counter riêng, nhưng query summary có chi phí theo dữ liệu. Badge local có thể tạm chưa chính xác khi đang lấy bù/PUT/đối chiếu; summary sửa lại khi cần, không polling hoặc GET sau mỗi tin.

Quy ước “đã đọc tới seq” đơn giản cho demo, không chứng minh người dùng đã xem từng tin. Unread còn lại không thể suy ra từ `last_seq - last_read_seq` vì có tin tự gửi, system và khoảng membership; web không đếm chỉ trên trang đã tải.
