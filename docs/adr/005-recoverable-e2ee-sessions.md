# ADR 005: E2EE theo phiên, khôi phục trên nhiều thiết bị

Ngày: 09/10/2026. Trạng thái: đã triển khai. Thay thế [ADR 004](004-e2ee-without-double-ratchet.md) trong runtime.

## Bối cảnh

Khóa chỉ nằm trong IndexedDB làm mất khả năng đọc tin khi đổi máy hoặc đóng phiên ẩn danh. X3DH cho từng tin và một tab sở hữu state không phù hợp với yêu cầu nhiều thiết bị, ít trạng thái và một mật khẩu.

## Quyết định

- Mỗi direct mới khởi tạo X3DH 3/4 DH một lần và dùng phiên cố định trên mọi thiết bị. HKDF stateless dẫn xuất khóa từng message từ SK và context gồm thread, epoch, UUID, sender, recipient. Không dùng Double Ratchet.
- Client dùng Argon2id rồi HKDF tách auth credential và vault key. Server bcrypt credential; mật khẩu gốc và vault key không gửi lên server.
- Account vault bất biến chứa private IK/SPK và 20 private OPK, được mã hóa trước khi lưu server. Public OPK claim bị xóa khỏi hàng đợi, còn vault và public bundle gốc được giữ nguyên.
- Sender lưu bootstrap và backup SK đã mã hóa trong cùng transaction tạo epoch. Recipient khôi phục private prekeys, xác minh SK và ghi backup riêng trước khi chấp nhận phiên. Backup gắn owner/thread/epoch/bootstrap và bất biến sau lần ghi đầu.
- Transaction khóa thread chọn phiên đầu tiên khi khởi tạo đồng thời. Đề xuất khác nhận `409` kèm phiên canonical để khôi phục. Đăng nhập/mở tab/đổi máy dùng lại phiên cố định; không có luồng thay phiên thủ công hay tự động. ID epoch, các bảng và API danh sách/backup vẫn giữ để đọc lịch sử tương thích đã có.
- Mọi thiết bị dùng cùng khóa tài khoản đã khôi phục; không giữ Web Lock độc quyền hay profile mutable chung. Realtime bao gồm các thiết bị của sender; retry giữ UUID và ciphertext.
- Direct chỉ `e2ee_v2`; nhóm plaintext. Chưa triển khai đổi mật khẩu.

## Đánh đổi

Lưu private prekeys và SK mã hóa phục vụ đọc lịch sử làm giảm bảo vệ từ việc xóa khóa. Khóa backup bị lộ có thể làm lộ lịch sử; thiết kế không có cơ chế phục hồi sau xâm nhập của Double Ratchet. Quên mật khẩu và không còn khóa đã mở thì không khôi phục được. Client cần HTTPS/WSS và frontend đáng tin cậy. Đây là profile riêng của dự án, không tuyên bố tương thích đầy đủ với Signal hay đã audit.

Argon2id dùng memory 65536 KiB, 3 lượt, parallelism 4, salt 16 byte và output 32 byte, theo [profile thứ hai trong RFC 9106](https://www.rfc-editor.org/rfc/rfc9106.html#section-4). Các primitive và ràng buộc giao thức được tham khảo từ [X3DH](https://signal.org/docs/specifications/x3dh/) và [HKDF RFC 5869](https://www.rfc-editor.org/rfc/rfc5869.html).

Wire contract và trình tự xử lý: [luồng E2EE](../e2ee-sequence.md). Schema: [ERD](../chat-api-erd.md).
