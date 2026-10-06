# ADR 004: X3DH cho mỗi tin, chưa dùng Double Ratchet

Ngày: 04/10/2026. Phạm vi: direct E2EE trên web.

## Bối cảnh

Recipient có thể offline sau khi đăng ký public bundle. Hệ thống cần mã hóa tại client và đọc lại lịch sử từ ciphertext lưu ở server.

## Quyết định

- Mỗi tin mới claim bundle và chạy X3DH riêng với X25519, HKDF-SHA-256, AES-256-GCM; tạo EK, nonce 12 byte và UUID mới.
- Chiều trả lời chạy lượt mới trong vai sender, không dùng key nhận từ tin trước. Retry giữ nguyên UUID/body/key, không claim hoặc mã hóa lại.
- Có OPK dùng 4DH; bundle không có OPK dùng 3DH. Header có OPK ID nhưng thiếu private OPK/cached key phải báo lỗi.
- Lưu message key và pending trong IndexedDB. Recipient chỉ xóa private OPK cùng transaction lưu key sau giải mã thành công.
- IK/SPK bất biến; bổ sung OPK dùng ID mới. Chữ ký dùng biến thể libsignal v0.2.2, chưa cam kết tương thích đầy đủ Signal.

Double Ratchet cần chains, DH ratchet, counters và skipped keys; các state này chưa nằm trong phạm vi triển khai. Xem [X3DH](https://signal.org/docs/specifications/x3dh/) và [Double Ratchet](https://signal.org/docs/specifications/doubleratchet/).

## Đánh đổi và giới hạn

| Vấn đề | Hệ quả |
| --- | --- |
| Mỗi tin claim bundle | Tăng request và tiêu thụ OPK; claim mất response không hoàn trả khóa |
| 3DH khi hết OPK | Lộ recipient IK/SPK cho phép tính lại key của lượt cũ từ transcript |
| Cache message key | Đọc lại lịch sử sau reload; lộ IndexedDB/RAM có thể lộ các tin đã cache |
| TOFU pin IK | Phát hiện thay đổi khóa về sau; lần đầu cần đối chiếu UUID/fingerprint qua kênh đã xác thực |
| Web client | XSS hoặc mã trang bị thay có thể lấy khóa/plaintext; WASM không cách ly bí mật khỏi mã cùng trang |
| Metadata | Server vẫn thấy các bên, UUID, seq, thời điểm, public header và độ dài ciphertext |

Có OPK chỉ bảo vệ lượt cũ khi private OPK/EK và message key không bị lấy, cùng giả định DH/KDF an toàn. Xóa record không chứng minh xóa vật lý khóa. X3DH từng tin không bảo đảm phục hồi sau khi client/khóa bị kiểm soát; chưa có Double Ratchet, multi-device, backup/khôi phục khóa hoặc E2EE nhóm.

Luồng triển khai: [X3DH](../e2ee-sequence.md). Cách chạy và kiểm tra: [README](../../README.md).
