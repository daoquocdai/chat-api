# Mini-Hermes Chat API

Mini-Hermes là demo chat dùng Go, Gin, PostgreSQL và sqlc. Chat 1-1 có web demo; group chat hiện có API REST và fan-out WebSocket. Người dùng đăng ký bằng mật khẩu, đăng nhập nhận JWT, chọn một tài khoản khác và trao đổi tin nhắn được lưu trong PostgreSQL.

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
| `POST` | `/threads/direct` | Tạo hoặc mở direct thread với `peer_id` |
| `POST` | `/threads/group` | Tạo nhóm với `name`, `member_ids`; creator là admin |
| `GET` | `/threads/:id/members` | Danh sách thành viên đang trong nhóm |
| `POST` | `/threads/:id/members` | Admin thêm thành viên bằng `user_id` |
| `DELETE` | `/threads/:id/members/:user_id` | Admin xóa thành viên khác |
| `POST` | `/threads/:id/leave` | Tự rời nhóm |
| `GET` | `/threads` | Danh sách direct/group thread đang tham gia của người gọi |
| `GET` | `/threads/:id/messages` | Một trang lịch sử theo cursor `before_seq`, `limit` |
| `POST` | `/threads/:id/messages` | Gửi tin plaintext |
| `PUT` | `/threads/:id/read` | Tăng read marker bằng `last_read_seq` |

JWT chứa external user UUID trong `sub`. Client không được chọn danh tính người thực hiện bằng `sender_id` hoặc `user_id`. `user_id` ở API thêm thành viên chỉ chọn tài khoản đích. Service tra actor nội bộ từ JWT; repository kiểm tra membership trong PostgreSQL. Gửi tin, đọc marker và quản lý thành viên cần membership đang hoạt động; lịch sử được giới hạn theo từng khoảng membership.

Gửi tin nhận `message_id` UUID và `content`. `message_id` chính là `messages.external_id`: client sinh một UUID cho mỗi lần gửi logic và giữ nguyên UUID lẫn payload khi retry. Retry bởi đúng người gửi, thread và nội dung trả lại tin đã lưu, không tăng `seq`; dùng lại UUID với dữ liệu khác trả `409 Conflict`. Response vẫn dùng trường `id` cho external message ID. Tin hệ thống khi tạo nhóm/thay đổi thành viên dùng UUID do PostgreSQL sinh, cùng bộ đếm `seq` với tin text.

`GET /threads/:id/messages` mặc định lấy 30 tin mới nhất, tối đa 100. API trả message theo `seq DESC` trong `{ "messages": [...], "next_cursor": ... }`; truyền `before_seq=<next_cursor>` để lấy trang cũ hơn. Danh sách thread và response tạo/mở thread có `last_read_seq`, `peer_last_read_seq` và `unread_count`. `unread_count` chỉ đếm message thực tế do người khác gửi, không tính system message.

`PUT /threads/:id/read` chỉ nhận `last_read_seq`; danh tính luôn đến từ JWT. Marker chỉ tăng, phải nằm trong phạm vi participant được xem và không vượt `threads.last_seq`.

## Group chat backend tuần 4

`POST /threads/group` nhận `{ "name": "Nhóm học", "member_ids": ["<Bob UUID>", "<Charlie UUID>"] }`. Creator tự được thêm với role `admin`; không đưa creator vào `member_ids`. Tên được trim, dài 1–100 ký tự Unicode; nhóm tối đa 100 thành viên kể cả creator. Danh sách UUID không được trùng. Tạo nhóm ghi thread, participants và system message `seq=1` trong một transaction. Cùng tên có thể tạo nhiều nhóm.

Admin thêm/xóa người khác; mọi thành viên được tự rời. Nếu admin cuối rời mà còn thành viên, người tham gia sớm nhất được nâng thành admin trong cùng transaction; nếu không còn ai, nhóm không còn xuất hiện trong danh sách active thread. Xóa chính mình dùng `/leave`.

Thay đổi membership và gửi tin cùng khóa dòng thread, nên không thể xen giữa việc kiểm tra quyền và cấp `seq`. Thêm người bắt đầu khoảng membership mới ở `joined_seq` của system message; rời/xóa kết thúc ở `left_seq` của system message. Người rời vẫn đọc được lịch sử trong các khoảng từng tham gia, gồm thông báo rời/xóa, nhưng không gửi tin, cập nhật read marker hoặc xem tin sau `left_seq`. Thêm lại không mở quyền xem tin trong khoảng vắng mặt. `/threads` chỉ liệt kê membership đang hoạt động; group có `name`, `role`, `member_count`, `peer: null`. `peer_last_read_seq` chỉ có ý nghĩa với direct thread.

Unread dùng `last_read_seq` của khoảng membership hiện tại, đếm tin người khác gửi và bỏ qua system message. Thêm/xóa/rời đã ở trạng thái đích không tạo thêm `seq`: API trả và thử publish lại system message của lần chuyển trạng thái hiện tại. Đây không phải idempotency cho tạo nhóm. Nếu tạo nhóm trả `503` do XADD, nhóm **đã lưu**; dùng `thread_id` trong lỗi hoặc `GET /threads` để tìm nhóm, không gửi lại POST tạo nhóm một cách mù quáng.

Bước này dùng các bảng hiện có, không cần migration mới. Backend có cache membership Redis được dùng khi publish; web demo vẫn chỉ hiển thị direct chat. API nhóm chạy thử bằng [docs/week4-groups.http](docs/week4-groups.http). Chưa có E2EE. Web nhận tin mới qua `ws-gateway`, không dò REST theo chu kỳ.

### Cache membership Redis

Cache nằm ở **API**, tại `internal/module/thread/membership/redis.go`. Nó lưu snapshot gồm `thread_id`, `version`, `member_ids` (external UUID của mọi thành viên được xem, gồm sender), dưới key `<redis.stream>:membership:<thread UUID>:<version>`, TTL 15 phút. `CachedPublisher` trong message service dùng cùng cache cho text và system message. Cache hit bỏ được query JOIN participants/users và việc lấy toàn bộ UUID từ PostgreSQL; kiểm tra quyền, khóa thread và query lấy version vẫn chạy trong PostgreSQL. API danh sách thành viên và đọc lịch sử không dùng cache này.

Version là mốc thay đổi danh sách người được xem, lấy từ các khoảng membership trong transaction: thêm có hiệu lực ở `joined_seq`, xóa/rời có hiệu lực ở `left_seq + 1`. Chọn mốc lớn nhất không vượt `message.seq`; không thêm cột version hoặc migration. Version gắn với snapshot lịch sử, không phải một key danh sách active bị ghi đè.

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
| `content_format` | `plaintext` hoặc định dạng hỗ trợ về sau |
| `content` | Nội dung đã lưu |
| `created_at` | UTC RFC3339 với độ chính xác nano giây |

Publisher không nhận recipient từ request. Với direct, repository lấy danh sách trong transaction; với group, repository trả version tại `seq` rồi service lấy snapshot từ cache/DB sau commit. Cả hai dùng cùng query khoảng `participants` bao phủ `seq`. Text bỏ sender khỏi danh sách; system message gửi tới mọi người trong khoảng đó, gồm actor/người vừa rời. Retry message cũ dùng membership tại `seq` cũ, không dùng danh sách thành viên hiện tại. `MembershipVersion` chỉ là metadata nội bộ trước publish, không phải field bắt buộc của stream. Gateway đọc `recipient_ids` và fan-out; entry cũ có `recipient_id` được đọc như danh sách một phần tử, thiếu `thread_kind` được hiểu là direct. Không phải xóa stream khi nâng cấp.

Nếu commit thất bại, request không có quyền hoặc UUID conflict thì không publish. Nếu commit đã thành công nhưng query snapshot fallback hoặc `XADD` lỗi/timeout, API trả `503 Service Unavailable` với hướng dẫn retry đúng `message_id` và payload cũ. Retry hợp lệ đọc lại cùng message/`seq` rồi thử publish lần nữa; gửi/retry vẫn yêu cầu sender đang là thành viên active. Vì kết quả XADD có thể đã tới Redis trước khi client nhận lỗi, stream có thể có nhiều entry cho cùng `message_id`. Gateway chuyển tiếp từng entry, kể cả entry trùng; client phải khử trùng theo `message_id` và dùng `seq` từ PostgreSQL để sắp thứ tự trong thread. Thiết kế này không bảo đảm exactly-once và không bảo đảm realtime nếu client không retry; chưa có outbox.

## WebSocket gateway và vé một lần

Chạy `chat-api` và gateway ở hai tiến trình, dùng cùng `config/config.yml`:

```powershell
go run ./cmd
go run ./cmd/ws-gateway
```

Gateway mặc định nghe tại `:8081` (`ws_address`); endpoint là `GET /ws`. Web dùng JWT hiện có trong header Bearer để gọi `POST /auth/ws-ticket`. API trả `{ "ticket": "...", "ws_url": "..." }`; vé ngẫu nhiên 32 byte có TTL 30 giây và không phải JWT. Web kết nối `ws_url?ticket=<vé>`. Gateway dùng Redis `GETDEL` để tiêu thụ vé nguyên tử **trước** khi upgrade, nên vé không thể dùng hai lần. CLI vẫn có thể gửi `Authorization: Bearer <JWT>` trực tiếp. Không đưa JWT dài hạn vào URL. Reverse proxy nên tránh ghi query string chứa vé vào log; production phải dùng `wss://` và cấu hình origin chính xác.

Gateway không kết nối PostgreSQL. Một user có thể mở nhiều kết nối; gateway duyệt danh sách recipient và mọi kết nối đang mở của từng người đều nhận JSON `message.created` với `message_id`, `thread_id`, `thread_kind`, `sender_id`, `recipient_id`, `seq`, `kind`, `content_format`, `content`, `created_at`. Kết nối chậm bị đóng khi hàng đợi đầy hoặc ghi/ping timeout; client cần tải bù qua REST.

Gateway còn gửi frame WebSocket `{"type":"heartbeat"}` mỗi 5 giây. Web không hiển thị frame này; tab đang hiện chỉ thay socket nếu 15 giây không nhận frame/event nào, hoặc handshake bị treo 10 giây. Tab ẩn không dùng watchdog heartbeat vì trình duyệt có thể trì hoãn JavaScript; khi hiện lại, socket vẫn báo OPEN được chờ thêm 5 giây để nhận heartbeat đang xếp hàng trước khi thay thế. Sự kiện mạng `online` cũng kiểm tra socket theo cách này. Request xin vé đang treo được hủy trước lần thử mới. Web gọi REST lấy bù khi mở thread, sau khi WebSocket mới **mở thành công**, hoặc khi event báo thiếu `seq`; không dò `GET /messages` định kỳ. Web cập nhật unread/last-seq từ message và read marker đã nhận; `GET /threads` làm mới trạng thái sau một gián đoạn kết nối, hoặc được gộp sau đợt gửi để cập nhật read marker của người kia (response gửi tin không chứa dữ liệu này). Khi gateway tắt, các lần thử kết nối dùng backoff tăng tới 30 giây và không lấy history trước khi socket mở.

Gateway tạo consumer group `ws-gateway` cho `redis.stream` nếu chưa có, từ ID `0` để không bỏ qua entry có trước lúc khởi động. Chỉ hỗ trợ **một gateway instance** và dùng consumer name cố định `gateway-1`. Khi khởi động/reconnect, nó đọc lại pending của chính consumer này bằng `XREADGROUP ... 0` trước khi đọc entry mới bằng `XREADGROUP ... >`. Nếu Redis tạm lỗi, gateway thử lại; không cần restart thủ công. Mỗi entry được `XACK` sau khi đã đưa vào hàng đợi kết nối hiện có; Bob offline cũng được `XACK`. `XACK` không xác nhận Bob đã nhận/đọc tin, và trường hợp gateway chết trước `XACK` có thể phát lại cùng event. PostgreSQL/REST vẫn là nguồn lịch sử tin nhắn.

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
- `prekeys` có schema để mentor review nhưng chưa có API hoặc nghiệp vụ E2EE.
- `participants.last_read_seq` lưu một read marker tăng đơn điệu; không tạo `is_read` trên từng message.

## Chạy thử thủ công

Sau khi migration thành công:

```powershell
go run ./cmd
go run ./cmd/ws-gateway  # chạy ở terminal thứ hai
```

Mở `http://localhost:8080`:

1. Đăng ký Alice và Bob.
2. Mở hai tab độc lập hoặc cửa sổ riêng tư, đăng nhập Alice và Bob, chọn nhau.
3. Alice gửi tin qua REST; Bob nhận ngay qua WebSocket. Ngắt gateway hoặc đưa Bob offline, gửi thêm hơn 30 tin, sau đó kết nối lại để kiểm tra web lấy bù hết các trang lịch sử.
4. Tải lại trang hoặc khởi động lại server; đăng nhập và chọn lại peer để xem lịch sử còn trong PostgreSQL.
5. Đăng nhập tài khoản thứ ba để xác nhận tài khoản đó không thể truy cập thread Alice–Bob bằng API.

Web lưu JWT trong `sessionStorage` của từng tab, dùng Bearer token cho REST và đổi vé ngắn hạn để mở WebSocket. Mỗi tin mới được chủ động gửi có một `message_id` mới, kể cả nội dung giống hệt; lỗi mạng hoặc 5xx được retry một lần bằng đúng UUID và payload. Nếu hai lần thử vẫn lỗi, web giữ tin chưa xác nhận trong cache của thread; nút **Gửi lại** tiếp tục dùng UUID và nội dung cũ cho đến khi thành công. Nếu người dùng sửa nội dung trong lúc đó, web chặn gửi và yêu cầu khôi phục nội dung cũ trước khi thử lại. Cache này chỉ tồn tại trong phiên trang, nên sau khi tải lại cần kiểm tra lịch sử trước khi gửi lại một tin chưa rõ kết quả. Tin mới đi qua WebSocket; khi mở thread, kết nối lại hoặc thấy gap `seq`, web lấy bù bằng REST và đi ngược `next_cursor` cho đến mốc đã biết. Tin từ REST/event được gộp theo `message_id`, render theo `seq`; nút **Tin cũ hơn** vẫn tải lịch sử cũ theo cursor. Web chỉ gửi read marker khi cuộc chat đang mở, tab đang hiển thị và các tin nhận liên tiếp đã thực sự xuất hiện trong viewport; PUT lỗi giữ lại mốc đã thấy để thử lại khi tab hiện hoặc kết nối phục hồi. Đăng xuất/đổi tài khoản đóng socket và hủy lịch reconnect cũ.

Collection nhóm: [docs/week4-groups.http](docs/week4-groups.http). Quyết định unread và thứ tự tin được ghi tại [ADR #2](docs/adr/002-unread-count.md) và [ADR #3](docs/adr/003-message-order.md).

Collection [docs/week2-chat.http](docs/week2-chat.http) minh họa đầy đủ hai người chat, request thiếu JWT và cả thao tác đọc/gửi bị từ chối với tài khoản thứ ba. Đổi biến `@run`, rồi chạy request từ trên xuống dưới.

Repo chỉ giữ test service cho user, thread, message và vé WebSocket. Chạy `go test ./...`, `go vet ./...`, `go build ./...` và `node --check web/app.js`. Gateway/web không còn test tự động thường trực; cần thử thủ công trên trình duyệt và với PostgreSQL/Redis khi thay đổi luồng tích hợp.

Với nhóm, cần kiểm tra thêm cache hit/miss và TTL, thêm/xóa/rời/thêm lại, gửi đồng thời với xóa thành viên, retry message cũ khi cache đã mất, pending cũ sau thay đổi membership, nhiều kết nối cùng user, lịch sử và unread trong từng khoảng. Dùng stream/cache riêng và tài khoản tạm cho kịch bản tích hợp; chỉ xóa dữ liệu/cổng/tiến trình của kịch bản, giữ nguyên dịch vụ có sẵn. Kiểm thử HTTP/WebSocket tự động bằng kịch bản tạm không thay thế demo giao diện nhóm trên trình duyệt.

Xem [docs/request-flow.md](docs/request-flow.md) để biết ranh giới handler/service/repository và transaction.

## Chưa có trong scope

- Giao diện nhóm trên web và demo nhóm đầy đủ.
- Nhiều gateway instance, E2EE, API prekey và chính sách retention/trim stream.

Để bổ sung group UI: hiển thị summary group (`name`, `role`, `member_count`, `peer: null`), thêm form tạo nhóm và màn hình thành viên, nối thao tác thêm/xóa/rời theo role. Sau đó cho client nhận event group, gộp text/system theo `message_id` và `seq`, lấy bù history bằng cursor và dùng read/unread hiện có. Cần xử lý 503 tạo nhóm bằng cách đối chiếu `thread_id`, đồng thời demo nhiều tab với thêm/xóa/thêm lại và reconnect.
