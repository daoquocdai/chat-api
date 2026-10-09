# E2EE khôi phục bằng một mật khẩu

Ngày thống nhất và triển khai: 09/10/2026. **Trạng thái: đã triển khai**; luồng hiện tại dùng `e2ee_v2`. Xem [luồng chi tiết](e2ee-sequence.md) và [ADR 005](adr/005-recoverable-e2ee-sessions.md). ADR 004 ghi lại thiết kế cũ.

## Yêu cầu đã thực hiện

1. Mỗi direct khởi tạo X3DH một lần và dùng chung phiên cố định trên mọi thiết bị; HKDF dẫn xuất khóa từng tin. Không dùng Double Ratchet.
2. Nhiều tab/thiết bị của cùng tài khoản có thể đồng thời đăng nhập, gửi, nhận và giải mã, không có tab sở hữu độc quyền.
3. Thiết bị, trình duyệt hay phiên ẩn danh mới khôi phục lịch sử bằng đúng username/mật khẩu, không cần dữ liệu của thiết bị cũ.

Direct luôn E2EE; nhóm plaintext. Giữ Go/WASM cho crypto. Đăng ký tự chuẩn bị E2EE; giao diện không có upload/refill hay takeover. Chưa triển khai đổi mật khẩu.

## Thiết kế hiện tại

- Client Argon2id với salt 16 byte, memory 65536 KiB, iterations 3, parallelism 4 tạo root 32 byte. HKDF tách `auth_credential` và `vault_key` theo hai mục đích riêng.
- Backend chỉ nhận credential dẫn xuất, bcrypt credential và lưu KDF công khai. Không nhận mật khẩu gốc, vault key hay private key dạng rõ.
- Account vault bất biến chứa private identity, signed prekey và 20 private one-time prekeys; AES-GCM mã hóa vault trước khi lưu server. Public bundle gốc không đổi khi hàng đợi public OPK bị tiêu thụ.
- X3DH tạo SK một lần cho direct mới. Sender lưu backup SK mã hóa cùng bootstrap trong transaction khởi tạo; recipient chưa có backup có thể tính SK từ private prekeys khôi phục trong vault, xác minh confirmation rồi lưu backup riêng.
- Backup SK dùng khóa dẫn xuất riêng từ vault key, ràng buộc owner/thread/epoch/bootstrap. Mỗi bên chỉ đọc backup của mình; backup đã ghi không bị ghi đè.
- Transaction khóa thread chọn phiên đầu tiên khi các thiết bị khởi tạo đồng thời. Client thua nhận `409` và dùng phiên canonical của server; đề xuất thay thế phiên đã có cũng bị từ chối.
- HKDF từ SK và context thread/epoch/message/sender/recipient tạo khóa AES-GCM từng tin; không có chain/counter mutable. Retry giữ nguyên UUID và ciphertext.
- Mỗi message tham chiếu epoch cụ thể. Mọi tin mới dùng phiên cố định đã có; không có luồng đổi phiên thủ công hoặc tự động. ID epoch, các bảng và API danh sách/backup vẫn giữ để đọc lịch sử tương thích đã có.
- Realtime gửi cả cho các thiết bị của sender. Client deduplicate UUID và lấy bù lịch sử bằng REST.
- Khóa đã mở nằm trong RAM; `sessionStorage` giữ JWT/username/vault key gắn với user UUID để reload. `localStorage` chỉ giữ ciphertext outbox theo UUID, có RAM fallback. Không dùng IndexedDB/Web Locks để bảo đảm phục hồi.

## Kiểm tra nghiệm thu

Kiểm tra nghiệm thu gồm crypto Go và WASM; HTTP/PostgreSQL/Redis/WebSocket thật; gửi đồng thời trên nhiều client; hai đề xuất khởi tạo chọn một phiên; dùng lại phiên trên mọi thiết bị và từ chối đề xuất thay thế bằng `409`; offline recipient đổi client trước lần giải mã đầu; khôi phục chỉ bằng mật khẩu trên client trống; đọc cả tin gửi/nhận; retry cùng UUID/seq; direct từ chối plaintext và group dùng plaintext.

Chạy lại bằng các lệnh trong [README](../README.md). Các kiểm tra tích hợp cần database local đã chạy migration và có tạo dữ liệu thử nghiệm.

## Phạm vi bảo đảm phục hồi

Đổi máy/tab không mất khả năng giải mã khi còn đúng mật khẩu và vault/epoch trên server. Quên mật khẩu mà không còn khóa đã mở thì không có đường khôi phục. Lưu archive SK/private prekeys là đánh đổi để đọc lịch sử; không có bảo đảm xóa khóa hoặc phục hồi sau xâm nhập của Double Ratchet. Giả định HTTPS/WSS và frontend được phân phối đáng tin cậy.
