# Luồng request Mini-Hermes

## Ranh giới các tầng

- DTO chỉ định nghĩa JSON HTTP; không chứa `password_hash` hoặc ID `BIGINT` nội bộ.
- Handler bind request, lấy external user ID đã xác thực từ Gin context và ánh xạ lỗi HTTP.
- Service chuẩn hóa username, kiểm tra input, băm/so mật khẩu và tra actor nội bộ; không phụ thuộc Gin.
- Repository dùng sqlc/PostgreSQL, kiểm tra participant và giữ transaction.
- JWT manager trong `internal/token` ký/xác minh token, không phụ thuộc Gin.
- Bearer middleware chỉ xác minh token và đặt external user ID từ `sub` vào context; không truy vấn database.

```mermaid
flowchart LR
    C[Web hoặc HTTP client] --> R[Gin router]
    R --> M[Bearer middleware]
    M --> H[Handler]
    H --> S[Service]
    S --> P[Repository]
    P --> Q[sqlc]
    Q --> DB[(PostgreSQL)]
```

## Auth

`POST /auth/register` dùng chung `NormalizeUsername`, kiểm tra password, băm bcrypt rồi ghi `password_hash`. Response không chứa mật khẩu hoặc hash.

`POST /auth/login` chuẩn hóa username, lấy credentials và so bcrypt. Sai username và sai password trả cùng `invalid username or password`. JWT HS256 chứa external UUID trong `sub`, cùng `iat` và `exp`; secret/TTL đến từ config runtime.

Một PostgreSQL user repository phục vụ cả đăng ký/đăng nhập, lookup user và danh sách user. Không còn constructor truyền cùng repository hai lần và không còn luồng tạo user thiếu password.

## Tạo hoặc mở direct thread

`POST /threads/direct` nhận `peer_id`; actor đến từ JWT.

1. Service tra actor và peer sang ID `BIGINT`, đồng thời chặn chat với chính mình.
2. Repository sắp cặp ID thành low/high, bắt đầu transaction và khóa hai dòng `users` theo đúng thứ tự tăng dần. PostgreSQL row lock dùng chung giữa mọi connection/process/instance; thứ tự toàn cục này tránh deadlock khi các cặp giao nhau.
3. Sau khi có đủ hai khóa, repository tìm direct thread có đúng hai active participant là cặp đó, bất kể ai gửi request trước.
4. Nếu chưa có, repository ghi thread và hai participant trong cùng transaction. Request đồng thời đợi khóa rồi đọc lại thread vừa commit.

Không còn cột cặp user hay unique index tương ứng trên `threads`. Vì vậy cơ chế chống trùng phụ thuộc mọi đường ghi direct thread đều dùng transaction/khóa trên; thao tác SQL trực tiếp hoặc implementation khác bỏ qua khóa có thể tạo trùng.

## Gửi tin

`POST /threads/:id/messages` nhận `message_id` và `content`, không nhận sender. `message_id` là UUID client đã chọn cho `messages.external_id`; response vẫn trả external ID trong `id`.

Trong một transaction, repository khóa thread đồng thời với việc xác nhận actor là participant đang hoạt động. Sau đó nó tra `external_id` toàn cục:

1. UUID đã thuộc đúng sender, thread, kind/format và nội dung: trả message cũ, không tăng `last_seq`.
2. UUID đã gắn với bất kỳ dữ liệu nào khác: trả `409 Conflict` chung, không trả message cũ nên không lộ nội dung/người gửi/thread của người khác.
3. UUID chưa có: tăng `last_seq`, ghi message với sequence đó và commit.

Unique constraint trên `messages.external_id` xử lý cả race giữa các thread/instance. Nếu insert thua race, toàn transaction (kể cả tăng sequence) rollback và API trả conflict. Hai request retry giống hệt trong cùng thread được serialize bởi khóa thread nên chỉ có một row.

Thread tồn tại nhưng actor không phải participant trả `403`; thread không tồn tại trả `404`.

## Đọc lịch sử

`GET /threads/:id/messages` tra actor từ JWT và chỉ query khi actor là participant đang hoạt động. `limit` mặc định 30, nằm trong `1..100`; `before_seq` nếu có phải dương. Repository query `seq DESC`, lấy `limit + 1`, giữ điều kiện `seq >= joined_seq` và không dùng `OFFSET`, timestamp hay message ID toàn cục.

```json
{
  "messages": [
    { "seq": 5, "content": "Tin 5" },
    { "seq": 4, "content": "Tin 4" }
  ],
  "next_cursor": 4
}
```

Trang tiếp theo gọi `?before_seq=4&limit=2` và chỉ nhận message có `seq < 4`. `next_cursor` là `null` khi không còn trang cũ hơn.

## Read marker và unread

`PUT /threads/:id/read` nhận `{ "last_read_seq": N }`; actor không bao giờ đến từ body. SQL chỉ update participant đang hoạt động khi `joined_seq - 1 <= N <= threads.last_seq`, đồng thời dùng `GREATEST(last_read_seq, N)`. Vì vậy request cũ đến muộn không thể làm marker giảm. Thread rỗng chấp nhận mốc `0`.

Hai query summary thread cùng trả `last_read_seq`, `peer_last_read_seq` và `unread_count`. `unread_count` là `COUNT(*)` trên các message nằm trong phạm vi xem, có `seq > last_read_seq`, do người khác gửi và `kind <> 'system'`; không suy ra từ `last_seq - last_read_seq`. UI dùng `peer_last_read_seq` để hiện “Đã đọc” cho tin mình gửi có `seq` không lớn hơn marker đó.

## Web demo

Router phục vụ `web/index.html`, `web/app.js` và `web/style.css`. Giao diện:

1. Đăng ký rồi đăng nhập.
2. Lưu JWT vào `sessionStorage` của tab và lấy user hiện tại từ claim `sub`.
3. Gọi `GET /users` bằng Bearer token và chỉ hiển thị các tài khoản khác.
4. Khi chọn peer, gọi `POST /threads/direct`, sau đó tải lịch sử.
5. Tải trang mới nhất, đảo sang thứ tự cũ → mới để render; nút **Tin cũ hơn** prepend trang theo `next_cursor` và map theo `seq` loại trùng.
6. Polling trang mới nhất mỗi 1,5 giây. Nếu trang đó chưa chạm `seq` cao nhất đã biết, web theo `next_cursor` cho đến khi chạm mốc, rồi merge; các trang cũ không bị thay thế.
7. Chỉ gọi `PUT read` cho chuỗi tin nhận đã xuất hiện trong viewport khi cuộc chat hiện tại và tab đều đang hiển thị. GET nền và thao tác gửi không tự cập nhật marker.
8. Mọi response bất đồng bộ đều đối chiếu session version, conversation version và thread ID trước khi cập nhật DOM; response muộn sau đổi thread/đăng xuất bị bỏ qua.
9. Khi API trả `401`, xóa session local và đưa người dùng về màn hình đăng nhập.

Không còn route `/messages` sender/receiver hoặc `POST /users`; client không có đường API để tự khai danh tính người gửi.
