# Mini-Hermes Chat API

Mini-Hermes là demo chat dùng Go, Gin, PostgreSQL và sqlc. Web hỗ trợ direct plaintext/E2EE và nhóm plaintext qua REST và fan-out WebSocket. E2EE chạy crypto Go/WASM tại browser; PostgreSQL lưu ciphertext. Người dùng đăng ký bằng mật khẩu, đăng nhập nhận JWT rồi chọn cuộc trò chuyện.

ERD 5 bảng vẫn đang chờ mentor duyệt. Migration mới trên nhánh `feat/auth` là bản thử nghiệm để review, chưa nên áp dụng cho môi trường dùng chung hoặc production.

## API

Public:

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/health` | Kiểm tra server |
| `POST` | `/auth/register` | Đăng ký với `username`, `password` |
| `POST` | `/auth/login` | Nhận JWT access token |

Yêu cầu `Authorization: Bearer <access_token>`:

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/users` | Danh sách tài khoản trong PostgreSQL |
| `POST` | `/auth/ws-ticket` | Cấp vé WebSocket ngắn hạn, dùng một lần |
| `POST` | `/threads/direct` | Tạo/mở direct với `peer_id`, tùy chọn `encryption_mode`; trả mode thực tế |
| `POST` | `/e2ee/prekeys` | Đăng ký/refill public IK/SPK/OPK của JWT actor |
| `POST` | `/e2ee/bundles/:user_id/claim` | Claim bundle peer trong direct E2EE qua `thread_id`, consume một public OPK nếu còn |
| `POST` | `/threads/group` | Tạo nhóm với `name`, `member_ids`; creator là admin |
| `GET` | `/threads/:id/members` | Danh sách thành viên đang trong nhóm |
| `POST` | `/threads/:id/members` | Admin thêm thành viên bằng `user_id` |
| `DELETE` | `/threads/:id/members/:user_id` | Admin xóa thành viên khác |
| `POST` | `/threads/:id/leave` | Tự rời nhóm |
| `GET` | `/threads` | Danh sách direct/group thread đang tham gia của người gọi |
| `GET` | `/threads/:id/messages` | Một trang lịch sử theo cursor `before_seq`, `limit` |
| `POST` | `/threads/:id/messages` | Gửi plaintext hoặc chuỗi envelope `e2ee_v1`, đúng mode thread |
| `PUT` | `/threads/:id/read` | Tăng read marker bằng `last_read_seq` |

JWT chứa external user UUID trong `sub`. Client không được chọn danh tính người thực hiện bằng `sender_id` hoặc `user_id`. `user_id` ở API thêm thành viên chỉ chọn tài khoản đích. Service tra actor nội bộ từ JWT; repository kiểm tra membership trong PostgreSQL. Gửi tin, đọc marker và quản lý thành viên cần membership đang hoạt động; lịch sử được giới hạn theo từng khoảng membership.

Gửi tin nhận `message_id` UUID và `content`. `message_id` chính là `messages.external_id`: client sinh một UUID cho mỗi lần gửi logic và giữ nguyên UUID lẫn payload khi retry. Retry bởi đúng người gửi, thread và nội dung trả lại tin đã lưu, không tăng `seq`; dùng lại UUID với dữ liệu khác trả `409 Conflict`. Response vẫn dùng trường `id` cho external message ID. Tin hệ thống khi tạo nhóm/thay đổi thành viên dùng UUID do PostgreSQL sinh, cùng bộ đếm `seq` với tin text.

`content_format` omitted/empty tương đương `plaintext`; direct E2EE chỉ nhận `e2ee_v1`. `content` E2EE là chuỗi JSON envelope được lưu nguyên byte. Direct mới omitted mode mặc định plaintext; direct đã có trả actual mode nếu omitted, yêu cầu mode khác trả 409. Không upgrade thread plaintext hoặc tạo cặp thứ hai. API chỉ xử lý public keys/header/quyền/retry, không nhận private key hay mã hóa/giải mã hộ. Xem [contract](docs/e2ee-contract.md) cho JSON và lỗi cố định.

`GET /threads/:id/messages` mặc định lấy 30 tin mới nhất, tối đa 100. API trả message theo `seq DESC` trong `{ "messages": [...], "next_cursor": ... }`; truyền `before_seq=<next_cursor>` để lấy trang cũ hơn. Danh sách thread và response tạo/mở thread có `last_read_seq`, `peer_last_read_seq` và `unread_count`. `unread_count` đếm cả text và system message do người khác tạo trong lần tham gia active; thao tác do chính mình tạo không tăng unread.

`PUT /threads/:id/read` chỉ nhận `last_read_seq`; danh tính luôn đến từ JWT. Marker chỉ tăng, phải nằm trong phạm vi participant được xem và không vượt `threads.last_seq`.

## Group chat backend

`POST /threads/group` nhận `{ "name": "Nhóm học", "member_ids": ["<Bob UUID>", "<Charlie UUID>"] }`. Creator tự được thêm với role `admin`; không đưa creator vào `member_ids`. Tên được trim, dài 1–100 ký tự Unicode; nhóm tối đa 100 thành viên kể cả creator. Danh sách UUID không được trùng. Tạo nhóm ghi thread, participants và system message `seq=1` trong một transaction. Cùng tên có thể tạo nhiều nhóm.

Admin thêm/xóa người khác; mọi thành viên được tự rời. Nếu admin cuối rời mà còn thành viên, người tham gia sớm nhất được nâng thành admin trong cùng transaction; nếu không còn ai, nhóm không còn xuất hiện trong danh sách active thread. Xóa chính mình dùng `/leave`.

Thay đổi membership và gửi tin cùng khóa dòng thread, nên không thể xen giữa việc kiểm tra quyền và cấp `seq`. Thêm người bắt đầu khoảng membership mới ở `joined_seq` của system message; rời/xóa kết thúc ở `left_seq` của system message. Người rời vẫn đọc được lịch sử trong các khoảng từng tham gia, gồm thông báo rời/xóa, nhưng không gửi tin, cập nhật read marker hoặc xem tin sau `left_seq`. Thêm lại không mở quyền xem tin trong khoảng vắng mặt. `/threads` chỉ liệt kê membership đang hoạt động; group có `name`, `role`, `member_count`, `peer: null`. `peer_last_read_seq` chỉ có ý nghĩa với direct thread.

Unread dùng `last_read_seq` của membership hiện tại, đếm text/system do người khác tạo. System message thêm người tại `joined_seq` tính unread cho người vừa được thêm nếu actor là người khác. Thêm/xóa/rời đã ở trạng thái đích không tạo thêm `seq`: API trả và thử publish lại system message của lần chuyển trạng thái hiện tại. Đây không phải idempotency cho tạo nhóm. Nếu tạo nhóm trả `503` do XADD, nhóm **đã lưu**; dùng `thread_id` trong lỗi hoặc `GET /threads` để tìm nhóm, không gửi lại POST tạo nhóm một cách mù quáng.

Bước này dùng các bảng hiện có, không cần migration mới. Backend giữ cache membership Redis khi publish; web dùng chung luồng thread cho direct và group. API nhóm và kịch bản thử được mô tả tại [luồng request](docs/request-flow.md) và phần chạy thử bên dưới. Direct E2EE dùng Go/WASM theo [demo E2EE](docs/e2ee-demo.md); group giữ plaintext. Web nhận tin mới qua `ws-gateway`, không dò REST theo chu kỳ.

### Cache membership Redis

Cache nằm ở **API**, tại `internal/module/thread/membership/redis.go`. Nó lưu snapshot gồm `thread_id`, `version`, `member_ids` (external UUID của mọi thành viên được xem, gồm sender), dưới key `<redis.stream>:membership:<thread UUID>:<version>`, TTL 15 phút. `message.Service.PublishMessage` dùng cùng cache cho text/system của direct và group. Cache hit bỏ được query JOIN participants/users và việc lấy toàn bộ UUID từ PostgreSQL; kiểm tra quyền/khóa thread nằm trong transaction PostgreSQL; scalar version được lấy theo seq sau commit. API danh sách thành viên và đọc lịch sử không dùng cache này.

Version là mốc thay đổi danh sách người được xem, lấy từ các khoảng membership sau commit tại seq của message: thêm có hiệu lực ở `joined_seq`, xóa/rời có hiệu lực ở `left_seq + 1`. Chọn mốc lớn nhất không vượt `message.seq`; không thêm cột version hoặc migration. Version gắn với snapshot lịch sử, không phải một key danh sách active bị ghi đè.

Ví dụ nhóm ban đầu có Alice/Bob ở seq 1, Bob bị xóa ở seq 5 và được thêm lại ở seq 9:

| Message | Version | Snapshot được dùng |
| --- | --- | --- |
| Text seq 2 hoặc retry seq 2 | 1 | Alice, Bob |
| System xóa Bob seq 5 | 1 | Alice, Bob; Bob được xem thông báo xóa |
| Text seq 6–8 | 6 | Alice |
| System thêm lại seq 9 và text sau đó | 9 | Alice, Bob |

Thêm/xóa/rời khiến các message sau chuyển sang version mới, nên không thể đọc nhầm key cũ. Snapshot cũ được giữ đến hết TTL để phục vụ retry, không cần DEL hoặc "current membership" pointer. Nếu cache miss, lỗi, sai kiểu hoặc JSON không hợp lệ, service đọc PostgreSQL theo **seq của message**, rồi thử ghi lại snapshot. Mỗi thao tác cache có timeout 100 ms trong tổng publish timeout; lỗi ghi cache không chặn publish bằng danh sách vừa lấy từ DB. Nếu query fallback hoặc XADD thất bại sau commit, API trả 503 với định danh message đã lưu; không phát event với danh sách người nhận chưa xác định.

Service loại sender khỏi snapshot đối với text; system giữ tất cả. Event trong stream vẫn chứa `recipient_ids` đầy đủ. Gateway không đọc cache, không query PostgreSQL và không thay người nhận theo membership hiện tại. Vì vậy entry cũ/pending vẫn dùng snapshot đã đóng gói; eviction cache không ảnh hưởng chúng.

## Redis Stream `message.created`

Sau khi transaction PostgreSQL của `POST /threads/:id/messages` commit thành công, service gọi `XADD` vào stream cấu hình tại `redis.stream` (mặc định mẫu: `mini-hermes:events`). Mỗi entry có `event=message.created` và các field:

| Field | Giá trị |
| --- | --- |
| `message_id` | External UUID của message, chính là `message_id` client gửi |
| `thread_id` | External UUID của thread |
| `sender_id` | External UUID của sender đã xác thực |
| `thread_kind` | `direct` hoặc `group` |
| `recipient_ids` | Mảng JSON external UUID của các thành viên có quyền xem ở `seq` của message |
| `seq` | Sequence trong thread |
| `kind` | `text` hoặc `system` |
| `content_format` | `plaintext` hoặc `e2ee_v1` |
| `content` | Nội dung đã lưu |
| `created_at` | UTC RFC3339 với độ chính xác nano giây |

Publisher không nhận recipient từ request. Repository chỉ lưu message trong transaction rồi commit. Message service dùng một đường chung cho direct/group: lấy version tại `message.seq`, lấy snapshot từ cache hoặc query khoảng `participants` bao phủ seq, tạo event rồi gọi publisher Redis XADD. Text bỏ sender khỏi danh sách; system message gửi tới mọi người trong khoảng đó, gồm actor/người vừa rời. Retry message cũ dùng membership tại `seq` cũ, không dùng danh sách thành viên hiện tại. Version chỉ dùng tại bước lấy snapshot, không truyền qua model message/event hoặc stream. Gateway đọc `recipient_ids` và fan-out; entry cũ có `recipient_id` được đọc như danh sách một phần tử, thiếu `thread_kind` được hiểu là direct. Không phải xóa stream khi nâng cấp.

Nếu commit thất bại, request không có quyền hoặc UUID conflict thì không publish. Nếu commit đã thành công nhưng query snapshot fallback hoặc `XADD` lỗi/timeout, API trả `503 Service Unavailable` với hướng dẫn retry đúng `message_id` và payload cũ. Retry hợp lệ đọc lại cùng message/`seq` rồi thử publish lần nữa; gửi/retry vẫn yêu cầu sender đang là thành viên active. Vì kết quả XADD có thể đã tới Redis trước khi client nhận lỗi, stream có thể có nhiều entry cho cùng `message_id`. Gateway chuyển tiếp từng entry, kể cả entry trùng; client phải khử trùng theo `message_id` và dùng `seq` từ PostgreSQL để sắp thứ tự trong thread. Thiết kế này không bảo đảm exactly-once và không bảo đảm realtime nếu client không retry; chưa có outbox.

## WebSocket gateway và vé một lần

Chạy `chat-api` và gateway ở hai tiến trình, dùng cùng `config/config.yml`:

```powershell
go run ./cmd
go run ./cmd/ws-gateway
```

Gateway mặc định nghe tại `:8081` (`ws_address`); endpoint là `GET /ws`. Web dùng JWT hiện có trong header Bearer để gọi `POST /auth/ws-ticket`. API trả `{ "ticket": "...", "ws_url": "..." }`; vé ngẫu nhiên 32 byte có TTL 30 giây và không phải JWT. Web kết nối `ws_url?ticket=<vé>`. Gateway dùng Redis `GETDEL` để tiêu thụ vé nguyên tử **trước** khi upgrade, nên vé không thể dùng hai lần. CLI vẫn có thể gửi `Authorization: Bearer <JWT>` trực tiếp. Không đưa JWT dài hạn vào URL. Reverse proxy nên tránh ghi query string chứa vé vào log; production phải dùng `wss://` và cấu hình origin chính xác.

Gateway không kết nối PostgreSQL. Một user có thể mở nhiều kết nối; gateway duyệt danh sách recipient và mọi kết nối đang mở của từng người đều nhận JSON `message.created` với `message_id`, `thread_id`, `thread_kind`, `sender_id`, `recipient_id`, `seq`, `kind`, `content_format`, `content`, `created_at`. Kết nối chậm bị đóng khi hàng đợi đầy hoặc ghi/ping timeout; client cần tải bù qua REST.

Gateway còn gửi frame WebSocket `{"type":"heartbeat"}` mỗi 5 giây. Web không hiển thị frame này; tab đang hiện chỉ thay socket nếu 15 giây không nhận frame/event nào, hoặc handshake bị treo 10 giây. Tab ẩn không dùng watchdog heartbeat vì trình duyệt có thể trì hoãn JavaScript; khi hiện lại, socket vẫn báo OPEN được chờ thêm 5 giây để nhận heartbeat đang xếp hàng trước khi thay thế. Sự kiện mạng `online` cũng kiểm tra socket theo cách này. Request xin vé đang treo được hủy trước lần thử mới. Web gọi REST lấy bù khi mở thread, sau khi WebSocket mới **mở thành công**, hoặc khi event báo thiếu `seq`; không dò `GET /messages` định kỳ. Web cập nhật unread/last-seq từ event; `GET /threads` đối chiếu unread sau PUT read thành công khi còn unread cần đếm lại hoặc server trả marker cao hơn mốc gửi, và sau gián đoạn kết nối. Gửi tin thành công cập nhật cache từ response, không tự GET summary; khi unread đã là 0, PUT tăng marker cũng không cần GET lại. Marker của người kia được cập nhật trong những lần lấy summary tiếp theo. Khi gateway tắt, các lần thử kết nối dùng backoff tăng tới 30 giây và không lấy history trước khi socket mở.

Gateway tạo consumer group `ws-gateway` cho `redis.stream` nếu chưa có, từ ID `0` để không bỏ qua entry có trước lúc khởi động. Chỉ hỗ trợ **một gateway instance** và dùng consumer name cố định `gateway-1`. Khi khởi động/reconnect, nó đọc lại pending của chính consumer này bằng `XREADGROUP ... 0` trước khi đọc entry mới bằng `XREADGROUP ... >`. Nếu Redis tạm lỗi, gateway thử lại; không cần restart thủ công. Mỗi entry được `XACK` sau khi đã đưa vào hàng đợi kết nối hiện có; Bob offline cũng được `XACK`. `XACK` không xác nhận Bob đã nhận/đọc tin, và trường hợp gateway chết trước `XACK` có thể phát lại cùng event. PostgreSQL/REST vẫn là nguồn lịch sử tin nhắn.

Khi nhận SIGINT/SIGTERM hoặc lỗi Serve, gateway chặn đăng ký mới tại Hub, hủy consumer và I/O của các kết nối, đóng socket song song rồi chờ HTTP, handler, vòng ghi/heartbeat và consumer trong cùng hạn 5 giây. Socket được đóng ngay, không chờ close handshake; client reconnect/lấy bù qua REST. Hết hạn thì đóng cưỡng bức HTTP/Redis còn lại để giải phóng I/O, chờ các vòng kết thúc rồi mới thoát main. Shutdown không giữ mutex Hub khi đóng mạng hoặc chờ goroutine. Xem [ADR #1](docs/adr/001-gateway-and-stream.md).

`ws_public_url` là URL mà browser nhìn thấy; mặc định local `ws://localhost:8081/ws`. `ws_allowed_origins` cho phép origin web `localhost:8080`/`127.0.0.1:8080` trong demo. Khi triển khai ở host khác, phải đổi cả hai giá trị. Vé và event đi qua Redis; gateway vẫn không cần repository PostgreSQL.

## Chuẩn bị database local

Sao chép cấu hình mẫu và thay JWT secret:

```powershell
Copy-Item config/config.yml.example config/config.yml
```

`config/config.yml` cần trỏ đến PostgreSQL local và có cấu hình tương tự:

```yaml
ws_address: ":8081"
ws_public_url: "ws://localhost:8081/ws"
ws_allowed_origins: ["localhost:8080", "127.0.0.1:8080"]
ws_ticket_ttl: 30s
auth:
  jwt_secret: "replace-with-a-long-random-secret"
  jwt_ttl: 24h
redis:
  address: "localhost:6379"
  password: ""
  database: 0
  stream: "mini-hermes:events"
  publish_timeout: 2s
```

Runtime từ chối khởi động nếu secret trống, TTL nhỏ hơn một giây, hoặc cấu hình Redis thiếu address/stream hay có publish timeout không dương. Khi khởi động, ứng dụng cũng `PING` PostgreSQL và Redis trước khi phục vụ HTTP.

### Cảnh báo dữ liệu legacy

Migration nền là `db/migrations/20260924090000_create_direct_chat_schema.sql`. Nó thêm `users.password_hash NOT NULL`, sau đó thay bảng `messages` sender/receiver cũ bằng schema theo thread. File này đã nằm trong lịch sử `origin/feat/auth`, vì vậy thay đổi bỏ các cột dư thừa nằm trong migration tiến hóa `db/migrations/20260928090000_use_external_message_id.sql` thay vì sửa migration đã công bố.

Câu `ALTER TABLE users ... password_hash NOT NULL` đứng đầu migration và không có default. Nếu còn user legacy, migration sẽ thất bại trước khi `DROP TABLE messages`; không có mật khẩu giả được tạo.

Chỉ khi chấp nhận bỏ dữ liệu trên database local thử nghiệm, chạy tường minh:

```powershell
docker compose up -d
docker compose exec postgres psql -U chat -d chat_api -c "TRUNCATE TABLE messages, users RESTART IDENTITY CASCADE;"
$env:DATABASE_URL = 'postgres://chat:chat@localhost:5432/chat_api?sslmode=disable'
goose -dir db/migrations postgres $env:DATABASE_URL up
```

Không chạy lệnh `TRUNCATE` trên database dùng chung, staging hoặc production. `goose down` chỉ phục hồi hình dạng schema cũ, không phục hồi row đã xóa hoặc message cũ đã bị thay thế.

Migration giữ năm bảng trong ERD và thêm các invariant cần cho demo:

- Direct thread không lặp lại cặp user trong `threads`; hai thành viên chỉ nằm trong `participants`.
- Repository sắp hai user theo ID, khóa hai dòng `users` theo thứ tự tăng dần trong transaction, tìm thread có đúng hai active participant rồi mới tạo thread và hai participant trong cùng transaction. Row lock có hiệu lực giữa nhiều connection/process/instance; thứ tự cố định tránh deadlock.
- Vì không còn unique index cho cặp direct user, tính duy nhất chỉ được bảo đảm khi mọi đường tạo thread đều đi qua transaction/row-lock trên. SQL thủ công hoặc code mới bỏ qua khóa vẫn có thể tạo trùng; mentor cần chấp nhận giới hạn này hoặc chọn một khóa tư vấn/quy tắc DB khác trước production.
- `threads.last_seq` được khóa, tăng và ghi message trong cùng transaction.
- `messages.external_id` có unique constraint toàn cục. Repository chỉ trả message cũ khi UUID khớp đồng thời sender, thread, kind/format và nội dung; mọi cách dùng lại khác trả conflict chung, không trả dữ liệu message đã tồn tại.
- `prekeys` chỉ lưu public material; upload/claim có user lock/transaction và watermark chống retry hồi sinh OPK đã consume. Private keys/message keys ở browser.
- `participants.last_read_seq` lưu một read marker tăng đơn điệu; không tạo `is_read` trên từng message.

## Chạy thử thủ công

Sau khi migration thành công:

```powershell
make run
make gateway  # chạy ở terminal thứ hai
```

Nếu `make` không có hoặc không chạy được trên Windows, dùng `go run ./cmd` và `go run ./cmd/ws-gateway` tương ứng. Với database local đã có schema, dùng `make up` để bật PostgreSQL/Redis và `make migrate-status` để kiểm tra migration; không cần reset dữ liệu cho tính năng này.

Mở `http://localhost:8080`:

1. Đăng ký Alice, Bob và Charlie; đăng nhập ở ba tab/cửa sổ riêng biệt.
2. Sidebar chia riêng **Chat 1-1** và **Chat nhóm**, mỗi cuộc trò chuyện có unread; nhóm hiển thị thêm số thành viên. Trong **Chat 1-1**, chọn tài khoản ở **Mở chat mới** nếu chưa có thread. Đổi qua lại thread đã tải để kiểm tra cache. Danh sách tự cập nhật qua realtime/reconnect, không có nút làm mới.
3. Alice nhập tên nhóm và chọn Bob/Charlie trong form **Tạo nhóm**. Bob/Charlie nhận system message tạo nhóm và nhóm xuất hiện trong danh sách; chọn nhóm để gửi/nhận tin. Nội dung hệ thống hiển thị riêng, text có tên người gửi.
4. Mở **Thành viên** để xem role. Alice có nút thêm/xóa người khác; mọi thành viên có nút **Rời nhóm**. Xóa Bob, gửi vài tin, rồi thêm Bob lại: Bob không thấy tin trong khoảng vắng mặt. Nhóm đã rời trong phiên hiện tại được ghi rõ **Đã rời (lịch sử)**, không còn unread/quyền gửi/quản lý.
5. Alice rời khi là admin cuối: thành viên tham gia sớm nhất còn lại được nâng thành admin. Đối chiếu nút quản lý ở tab đó; quyền do API quyết định, không suy luận từ câu chữ system message.
6. Cho Bob offline hoặc tắt `make gateway`, gửi hơn 30 tin rồi bật lại gateway. Web lấy bù qua nhiều trang; nút **Tin cũ hơn** tải phần lịch sử cũ. Chuyển tab liên tục khi gateway tắt để kiểm tra backoff, không xin thêm vé ngoài lịch retry.
7. Kiểm tra read marker theo quy ước mở thread tại tin mới nhất: cache chưa tải chụp `last_read_seq` từ summary, lấy trang mới nhất rồi đi lùi bằng cursor đến mốc đó hoặc hết lịch sử được phép; mốc 0 lấy đến hết cursor. Với 45 unread, phải lấy đủ phần bỏ lỡ qua nhiều trang trước khi initial sync hoàn tất; page lỗi giữ trạng thái chưa hoàn tất để mở lại thread/reconnect thử lại. Tab visible, chat active và cuối chat trong viewport mới đánh dấu tới seq đã render, không vượt `syncedSeq`; E2EE còn chờ decrypt/local commit. **Tin cũ hơn** tiếp tục từ trang cũ nhất đã lấy. Cache đã tải được dùng lại khi đổi thread. Cuộn lên rồi nhận tin mới không tự kéo xuống hoặc đánh dấu đọc; quay về cuối mới tăng marker. Tab ẩn/thread khác không cập nhật marker. Đây là quy ước “đã đọc”, không chứng minh đã xem từng tin; nhóm không dùng trạng thái peer direct.
8. Nếu tạo nhóm trả 503 kèm `thread_id`, web chỉ GET đối chiếu nhóm đã lưu và mở nó; không tự POST tạo lại. Nếu chưa đối chiếu được, dùng **Đối chiếu nhóm đã tạo**. Khi lỗi mạng không có ID, web giữ trạng thái chưa xác nhận: kiểm tra danh sách nhóm trước khi tải lại trang và tạo nhóm khác.

Để đo request đọc ngay: Bob mở direct chat với Alice, giữ tab visible ở cuối chat. Sau khi tải ban đầu và WebSocket đã mở, xóa log Network ở tab Bob. Alice gửi 3 tin lần lượt, đợi PUT read của Bob hoàn tất trước tin tiếp theo. Khi mỗi marker đã phủ last seq đang biết, kỳ vọng **3 PUT `/read`, 0 GET `/threads` phát sinh từ việc đọc**; không tính request tải ban đầu/reconnect. Để demo lấy bù trong nhóm, Charlie mở nhóm trước rồi ngắt mạng mà không tải lại trang, Alice/Bob gửi hơn 30 tin, bật mạng lại: cache giữ `syncedSeq`, tải trang mới nhất rồi lùi bằng `before_seq`, không trùng message ID.

Web lưu JWT trong `sessionStorage` của từng tab, dùng Bearer token cho REST và đổi vé ngắn hạn để mở WebSocket. Với plaintext, mỗi tin mới có UUID mới; lỗi mạng hoặc 5xx được retry một lần bằng đúng UUID và payload. Nếu hai lần thử vẫn lỗi, web giữ pending trong RAM của thread; nút **Gửi lại** tiếp tục dùng UUID/nội dung cũ. Sửa ô nhập không sửa pending; reload cần đối chiếu lịch sử trước khi gửi lại tin chưa rõ kết quả. Với E2EE, pending body/key được commit IndexedDB trước POST và còn sau reload; **Gửi lại pending** dùng nguyên body/UUID, không claim/seal lại. Tin mới đi qua WebSocket; khi mở thread, kết nối lại hoặc thấy gap `seq`, web lấy bù bằng REST/cursor, gộp UUID và render theo seq. **Tin cũ hơn** giữ pagination. Read marker chỉ tới tin đã render trong chat active/tab visible/cuối viewport; E2EE còn yêu cầu decrypt/local commit và tất cả loaded ciphertext trước mốc đó không pending/error. PUT lỗi giữ mốc để thử lại tại trigger phù hợp, không polling. Snapshot phiên/membership loại callback cũ; logout đóng socket/hủy HTTP và giải phóng Web Lock sau khi local state đã settle.

Quyết định kiến trúc được ghi tại [ADR #1: gateway/stream](docs/adr/001-gateway-and-stream.md), [ADR #2: unread](docs/adr/002-unread-count.md), [ADR #3: thứ tự tin](docs/adr/003-message-order.md) và [ADR #4: E2EE mỗi message](docs/adr/004-e2ee-without-double-ratchet.md). Khi demo ba tài khoản, kiểm tra thêm request thiếu JWT trả 401 và tài khoản không tham gia thread bị từ chối gửi/đọc marker.

Repo chỉ giữ sáu test files theo [AGENTS.md](AGENTS.md), gồm crypto/E2EE service và bốn service test cũ. Chạy `make test`, `make build`, `make wasm`, `go vet ./...` và `node --check` cho app/realtime-core/e2ee/e2ee-state/e2ee-wasm. Dùng `go test -count=1 ./...` khi cần evidence không cache. Nếu Make không chạy được, dùng Go/PowerShell tương đương trong [demo E2EE](docs/e2ee-demo.md). Gateway/web dùng kiểm chứng browser hoặc script tạm rồi xóa; native/Node mock không thay browser và PostgreSQL/Redis thật.

Với nhóm, cần kiểm tra thêm cache hit/miss và TTL, thêm/xóa/rời/thêm lại, gửi đồng thời với xóa thành viên, retry message cũ khi cache đã mất, pending cũ sau thay đổi membership, nhiều kết nối cùng user, lịch sử và unread trong từng khoảng. Dùng stream/cache riêng và tài khoản tạm cho kịch bản tích hợp; chỉ xóa dữ liệu/cổng/tiến trình của kịch bản, giữ nguyên dịch vụ có sẵn. Kiểm thử HTTP/WebSocket tự động bằng kịch bản tạm không thay thế demo giao diện nhóm trên trình duyệt.

Xem [docs/request-flow.md](docs/request-flow.md) để biết ranh giới handler/service/repository và transaction.

## E2EE direct trên web

Chạy `make wasm` trước demo; hai artifacts được build từ cùng compiler Go và ignored. Mở hai profile độc lập, mỗi tài khoản một client E2EE active; init/register public bundle, chọn E2EE cho direct mới, gửi/nhận ngay trong chat. IndexedDB phân vùng API URL/user UUID và Web Locks chọn một owner tab. Hướng dẫn hai chiều, reload/offline/lấy bù, pending/fingerprint, query SQL ciphertext và evidence thực tế tại [docs/e2ee-demo.md](docs/e2ee-demo.md).

Sơ đồ riêng để nộp mentor tại [docs/e2ee-sequence.md](docs/e2ee-sequence.md): đăng ký/claim public bundle và X3DH/encrypt/send/decrypt trên web, cùng phạm vi một profile/tab E2EE active cho mỗi tài khoản. Sơ đồ chưa được xác nhận duyệt.

## Chưa có trong scope

- Nhiều gateway instance và chính sách retention/trim stream.
- Double Ratchet, rotation/backup/key recovery, multi-device và E2EE nhóm. Giới hạn của X3DH từng tin, message-key cache, TOFU và client web được ghi tại ADR #4.
