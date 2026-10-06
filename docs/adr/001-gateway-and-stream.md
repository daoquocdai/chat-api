# ADR 001: Tách API, gateway và Redis Stream

## Bối cảnh

HTTP API xác thực người dùng và ghi chat vào PostgreSQL. WebSocket giữ kết nối lâu dài để báo tin mới. Demo tuần 3–4 cần hai luồng dễ hiểu và có thể khởi động/dừng riêng.

## Quyết định

- Tách HTTP API và WebSocket gateway thành hai tiến trình. API theo handler → service → repository; gateway không kết nối PostgreSQL, chỉ xác thực kết nối và fan-out.
- Sau transaction commit, message service publish `message.created` qua Redis Stream. Text và system dùng chung luồng publish. Cache membership nằm ở API, theo version lịch sử tại `message.seq`; event đóng gói `recipient_ids`. Gateway dùng nguyên danh sách đó, kể cả entry cũ/pending, không tra membership hiện tại.
- Phạm vi hiện tại là **một gateway instance**, group `ws-gateway`, consumer cố định `gateway-1`. Khi khởi động/reconnect, đọc pending của consumer bằng `XREADGROUP ... 0` trước entry mới bằng `>`.
- `XACK` sau khi xử lý/xếp event vào hàng đợi các kết nối hiện có, kể cả khi người nhận offline. ACK không xác nhận WebSocket đã giao tin, client đã nhận hay người dùng đã đọc. Kết nối chậm bị đóng; REST/PostgreSQL là nguồn lấy bù.
- Shutdown chặn đăng ký mới trong Hub, hủy consumer và I/O của kết nối, đóng socket song song rồi chờ HTTP, handler, vòng ghi/heartbeat và consumer trong cùng hạn 5 giây. Không đợi close handshake từng socket. Hết hạn thì đóng HTTP/Redis còn lại để giải phóng I/O, chờ các vòng kết thúc rồi thoát.

## Đánh đổi

Redis giúp tách việc lưu tin khỏi các kết nối realtime, nhưng thêm một dịch vụ và không bảo đảm exactly-once. Gateway chết trước ACK hoặc API retry publish có thể tạo event lặp; client gộp theo message ID.

Có khoảng hở **commit → XADD**: tin đã lưu nhưng publish lỗi, hoặc API chết trước publish. Text trả 503 khi nhận được lỗi sau commit; retry đúng UUID/payload không tăng seq nhưng thử publish lại. Tạo nhóm lỗi publish đã có nhóm, cần đối chiếu bằng thread ID/danh sách thay vì POST tạo lại. REST vẫn lấy được tin đã commit; chưa có outbox nên không bảo đảm mọi tin đều xuất hiện realtime.

Consumer group hiện tại không dùng để fan-out giữa nhiều gateway. Mở rộng nhiều instance cần quyết định khác. Dừng gateway chủ động ngắt socket; client reconnect và lấy bù, không cố giữ hàng đợi realtime trong RAM.
