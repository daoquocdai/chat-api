# ADR 001: Tách API, gateway và Redis Stream

## Bối cảnh

HTTP API lưu tin; WebSocket giữ kết nối dài hạn. Hai thành phần cần vòng đời và trách nhiệm riêng.

## Quyết định

- API và gateway chạy riêng; gateway không kết nối PostgreSQL.
- Sau commit, API publish `message.created` với snapshot `recipient_ids` tại message seq.
- Một gateway instance, group `ws-gateway`, consumer `gateway-1`; đọc pending bằng `XREADGROUP ... 0` trước entry mới bằng `>`.
- `XACK` sau xử lý, kể cả recipient offline; không xác nhận giao tin/đọc tin. Client lấy bù REST.

## Đánh đổi

Commit và XADD không nguyên tử; chưa có outbox. Publish lỗi trả `503`; retry giữ UUID/payload nhưng có thể phát event trùng. Client phải khử trùng UUID. Chi tiết: [luồng xử lý](../request-flow.md).
