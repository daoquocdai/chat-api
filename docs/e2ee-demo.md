# Demo E2EE web — tuần 5

Luồng hai chiều dùng giao diện Mini-Hermes, crypto Go/WASM, REST gửi, WebSocket nhận và REST lịch sử/lấy bù. Hợp đồng cố định nằm trong [e2ee-contract.md](e2ee-contract.md); quyết định và điều kiện bảo mật nằm trong [ADR #4](adr/004-e2ee-without-double-ratchet.md). Controller dùng sender/recipient tổng quát, mỗi tin mới có lượt X3DH riêng.

File sơ đồ riêng để nộp: [e2ee-sequence.md](e2ee-sequence.md), gồm đăng ký/claim bundle và tính secret/mã hóa/gửi/giải mã; sơ đồ tổng thể cũng có ở [contract, phần II.2](e2ee-contract.md#2-sequence-diagram-web-rest-websocket-và-gowasm).

**Phạm vi demo đề xuất để mentor xem xét:** chat direct E2EE A→B và B→A trên web, mỗi tài khoản dùng một browser profile cố định và một tab E2EE hoạt động. Web Lock chọn owner trong cùng origin/profile; tab thứ hai chưa decrypt/send/read E2EE cho đến khi tiếp quản. Phạm vi này chưa được mentor xác nhận duyệt.

Client giữ private keys, tính secret và mã hóa trước POST. Server lưu/truyền public bundle, ciphertext/envelope và metadata. REST gửi, WebSocket nhận, REST lấy bù; mỗi tin mới dùng lượt X3DH mới, retry giữ nguyên UUID/payload. Demo khởi tạo 20 OPK; hết OPK dùng 3DH với giới hạn trong ADR #4.

Plaintext direct/group giữ luồng hiện có, không chuyển thread plaintext cũ sang E2EE. Chưa làm Double Ratchet, E2EE nhóm, multi-device, backup/rotation hoặc đồng bộ khóa giữa tab. Gateway fan-out tới nhiều connections của recipient không đồng nghĩa nhiều tab cùng decrypt; text không echo sang connections khác của sender, nên chưa có đồng bộ đầy đủ các tab cùng tài khoản.

## Chạy demo

Dùng cấu hình local và schema đã có như README; PostgreSQL và Redis phải chạy. Build ở thư mục repo:

```powershell
make test
go test -count=1 ./internal/e2ee ./internal/module/e2ee/service ./internal/module/thread/service ./internal/module/message/service ./internal/module/user/service ./internal/wsticket
make build
make wasm
make run
```

Chạy `make gateway` trong terminal thứ hai. `make wasm` tạo `web/e2ee.wasm` và chép `wasm_exec.js` từ cùng compiler Go; hai artifact được ignore, cần build trước khi mở web. Web/API cùng origin, `ws_public_url` và `ws_allowed_origins` theo README. Dùng HTTPS hoặc localhost được browser coi là secure context, có IndexedDB, Web Locks và WebAssembly.

Nếu Make không có trên Windows, dùng `go test ./...`, lệnh `-count=1` trên, `go build ./...`, rồi build artifact bằng PowerShell:

```powershell
$p5PreviousGOOS = $env:GOOS
$p5PreviousGOARCH = $env:GOARCH
try {
    $env:GOOS = 'js'
    $env:GOARCH = 'wasm'
    go build -o web/e2ee.wasm ./cmd/e2ee-wasm
    if ($LASTEXITCODE -ne 0) { throw 'Build WASM failed' }
} finally {
    $env:GOOS = $p5PreviousGOOS
    $env:GOARCH = $p5PreviousGOARCH
}
Copy-Item -LiteralPath (Join-Path (go env GOROOT) 'lib/wasm/wasm_exec.js') -Destination web/wasm_exec.js
go run ./cmd
```

Ở terminal thứ hai dùng `go run ./cmd/ws-gateway`. `go vet ./...` và `node --check` cho `web/app.js`, `web/realtime-core.js`, `web/e2ee.js`, `web/e2ee-state.js`, `web/e2ee-wasm.js` là kiểm tra bổ sung. Không chạy migration/reset cho demo trên schema hiện tại.

1. Mở web bằng **hai browser profile độc lập**, đăng ký hai tài khoản mới A và B. Giữ đúng một profile E2EE cho mỗi tài khoản. Cặp đã có direct plaintext không chuyển sang E2EE.
2. Sau đăng nhập, chờ trạng thái khôi phục. Với tài khoản mới chưa đăng ký E2EE, bấm **Khởi tạo E2EE cho tài khoản mới**, rồi **Đăng ký public bundle** ở mỗi profile. Khóa và payload đăng ký được lưu trước HTTP; chỉ public bundle được upload. Chờ **Sẵn sàng**.
3. Ở A chọn **E2EE** trong chế độ cuộc trò chuyện mới, rồi chọn B. Gửi từ ô chat. B mở direct tương ứng, nhận event và thấy plaintext sau decrypt cùng local commit. B trả lời từ cùng ô chat; A nhận WebSocket và decrypt. Chiều B→A claim bundle A, tạo EK/nonce/UUID/key mới; không dùng key nhận từ A. Gửi xen kẽ nhiều tin, hai phía hiển thị cùng thứ tự seq và không trùng dòng.
4. So UUID và fingerprint của mình với fingerprint peer ở phía còn lại qua kênh tin cậy. Peer chưa biết được ghi rõ; fingerprint peer có sau lần gửi/nhận hợp lệ đầu tiên. Xem fingerprint không claim OPK. TOFU lần đầu chưa bảo vệ khỏi MITM; identity thay đổi chặn thao tác và không ghi đè pin.
5. Ở DevTools Network của A, kiểm tra POST `/threads/<UUID>/messages`: `content_format=e2ee_v1`, `content` là chuỗi JSON envelope chứa ciphertext. Nội dung tin không nằm dưới dạng plaintext trong body. Upload/claim chỉ chứa public keys và metadata; không có private key hoặc message key.
6. Query PostgreSQL theo UUID tin/thread để xác nhận `content_format`, envelope và ciphertext; so nguyên chuỗi `content` với request/response. Server, Redis và gateway giữ opaque content, không mã hóa/giải mã. Không đưa private keys/JWT hoặc toàn bộ profile vào log/query.
7. Reload cả A và B trong đúng profile, mở lại thread: cache mới chụp `last_read_seq` từ summary rồi lấy trang mới nhất và đi lùi bằng cursor đến mốc đó hoặc hết lịch sử được phép; mốc 0 lấy đến hết cursor. Read marker chờ initial sync thành công và decrypt/local commit/render. Trang lỗi giữ trạng thái chưa hoàn tất; mở lại thread hoặc reconnect để thử lại. **Tin cũ hơn** tiếp tục từ trang cũ nhất đã tải, dành cho lịch sử trước phần vừa lấy bù. Own/peer history dùng cached message key nếu đã lưu dù private OPK đã xóa; tin chưa cache dùng bộ private prekeys local. Ngắt mạng B khi thread đã mở, A gửi thêm tin, rồi kết nối lại: socket mở lại kích hoạt REST lấy bù và decrypt. Khi gateway không phát event, mở thread vẫn lấy REST; không polling.
8. Mở tab thứ hai cùng profile, API URL và tài khoản A: trạng thái **E2EE đang dùng ở tab khác**, không decrypt/send/refill/retry/read E2EE. Đóng hoặc đăng xuất tab sở hữu, rồi bấm **Thử tiếp quản E2EE** ở tab còn lại. Plaintext/group vẫn hoạt động ở tab không sở hữu.
9. Để thử retry, làm response POST bị mất sau khi DB đã lưu, giữ tab hoặc reload cùng profile. Banner pending hiển thị UUID/thread. Bấm **Gửi lại pending**: nguyên body/UUID/envelope/nonce được dùng lại, không claim/seal lại; DB vẫn một row và cùng seq. **Đối chiếu lịch sử** có thể xác nhận payload đã lưu. **Hủy pending này** bỏ pending nhưng giữ cached key vì tin có thể đã commit; không chứng minh server chưa nhận tin.
10. **Bổ sung 20 OPK** lưu batch private và request trước upload. Nếu upload chưa xác nhận, gửi lại bằng **Gửi lại public bundle**, giữ signature/ID/payload cũ. Watermark là ID cao nhất đã xác nhận; số OPK server là số tại response gần nhất, có thể đã giảm sau claim.
11. Với hai tài khoản demo mới ban đầu mỗi phía 20 OPK, gửi ít nhất 21 tin **mỗi chiều** mà chưa refill. Các tin đầu có `one_time_prekey_id` dương (4DH); khi public OPK recipient hết, claim trả null và envelope có ID null (3DH). Xác nhận qua query dưới đây, không suy từ count trên UI vì đó là số tại lần upload gần nhất. OPK cũng có thể hao nếu claim xong sender dừng hoặc response mất. Không xóa khóa/DB bằng tay để giả chứng minh 3DH.

## Query PostgreSQL ciphertext

Lấy UUID thread demo từ URL request `/threads/<UUID>/messages` hoặc response tạo thread. Query chỉ đọc đúng thread đó; không in profile/JWT/private key/shared secret. Chạy bằng psql trong container local (hoặc psql trực tiếp theo cấu hình DB):

```powershell
$p5DemoThreadID = '<UUID thread E2EE demo>'
@'
SELECT t.external_id AS thread_id, t.encryption_mode,
       m.external_id AS message_id, u.external_id AS sender_id,
       m.seq, m.content_format, octet_length(m.content) AS content_bytes,
       m.content::jsonb ->> 'ciphertext' AS ciphertext,
       m.content::jsonb ->> 'ephemeral_key' AS ephemeral_public_key,
       m.content::jsonb ->> 'nonce' AS nonce,
       m.content::jsonb ->> 'one_time_prekey_id' AS one_time_prekey_id
FROM messages m
JOIN threads t ON t.id = m.thread_id
JOIN users u ON u.id = m.sender_id
WHERE t.external_id = :'thread_id'::uuid
  AND m.content_format = 'e2ee_v1'
ORDER BY m.seq;
'@ | docker compose exec -T postgres psql -U chat -d chat_api -v "thread_id=$p5DemoThreadID"
```

UUID/seq phải khớp response/history và `content_format=e2ee_v1`; `content` chứa envelope/ciphertext, không phải plaintext của chat. EK/nonce mới khác ở các UUID mới, có hai sender trong cùng thread; retry cùng UUID giữ nguyên ciphertext/nonce/seq. Query public transcript không chứng minh key bí mật khác nhau hay crypto an toàn; evidence browser và native được ghi riêng bên dưới. Không in message keys để so sánh.

Các trạng thái **Đang khởi tạo/khôi phục**, **Sẵn sàng**, **Thiếu khóa**, **E2EE đang dùng ở tab khác** và **Lỗi E2EE** có điều kiện riêng. Tin chưa decrypt hoặc commit thành công có placeholder/lỗi; không tự đánh dấu đã đọc. Mốc đọc chỉ tăng sau plaintext đã decrypt, commit và render, trong thread active/tab visible/ở cuối viewport, không vượt tin pending/error đã tải. Nút **Thử giải mã lại tin đang lỗi** xử lý lại ciphertext đã giữ trong RAM.

Mất IndexedDB không khôi phục được khóa cũ. Không khởi tạo để thay IK/SPK của tài khoản đã đăng ký; dùng tài khoản demo mới khi cần làm lại. Web Locks chỉ điều phối các tab cùng origin/profile. Private keys trong IndexedDB/RAM có thể bị lấy bởi XSS hoặc mã trang/WASM bị thay; WASM không bảo vệ khóa khỏi mã độc trong cùng trang. Không có nhiều thiết bị, backup/export/import hoặc key rotation trong bước này.

## Evidence Prompt 4 — lịch sử

Đây là checkpoint A→B trước Prompt 5; các mục chưa chạy trong phần này mô tả trạng thái tại checkpoint đó. Kết quả mới nhất nằm ở phần Prompt 5 bên dưới và cuối [e2ee-contract.md](e2ee-contract.md). Các kết quả Node có mock storage/API được phân biệt với Edge thật và PostgreSQL/Redis/gateway thật; build không chứng minh browser demo PASS.

Đã chạy ngày **04/10/2026**, trên branch `feat/e2ee`, HEAD `b5260e8f`, Windows amd64, Go 1.27.1, Node 24.11.1 và Edge 154.0.4258.53 headless qua CDP. Hai browser profiles thật dùng IndexedDB/Web Locks thật; API/router/JWT/services/repositories, PostgreSQL 16.15, Redis và gateway đều thật, trên cổng và namespace fixture riêng. Node chỉ điều khiển browser; crypto chạy bằng Go/WASM tại Edge.

| Kiểm chứng | Kết quả thực tế |
| --- | --- |
| `make test`, `make build`, `make wasm`, `go vet ./...`, `gofmt -l internal/route/router.go` | PASS; Go unit tests hiện có có dùng cache; build khác browser evidence |
| `node --check` năm JS mới/đổi | PASS |
| `node .tmp-e2ee-p4/browser-demo.cjs` + `node .tmp-e2ee-p4/browser-followup.cjs` | PASS **63 assertions** trên Edge thật: 50 suite và 13 follow-up; helpers tạm theo AGENTS.md |
| A/B init/register → direct E2EE → A gửi REST → B nhận WebSocket | PASS; B decrypt/commit/render plaintext và private OPK chỉ xóa sau local commit; cả sáu bridge methods đã gọi tại browser |
| Request và PostgreSQL | PASS; POST chứa envelope/ciphertext, không chứa plaintext của tin, SHA-256 nguyên content DB khớp request. Cuối demo **5 E2EE rows, seq 1–5, last_seq 5** |
| B reload/history và offline/reconnect/REST catch-up | PASS, cached key đọc lại lịch sử sau OPK delete, không polling |
| Retry sau response mất | PASS; 6 POST attempts/5 UUIDs, hai body hashes của UUID retry giống nhau, không claim lại hoặc thêm row/seq |
| Abort local khi gửi/nhận và read marker | PASS; sender save abort không POST; receiver abort giữ OPK/cache, không plaintext/read; reload decrypt/commit/render rồi marker tăng. Hai markers cuối đều 5 |
| TOFU, duplicate, scope và nhiều tab | PASS; pin đổi không overwrite/consume; UUID/event/REST không trùng dòng/unread; cross-thread UUID chặn trước merge; tab hai bị lock, owner logout rồi tiếp quản chủ động; đổi account không lộ state cũ |
| B refill/upload retry | PASS; 20 OPK IDs 22–41 đã lưu trước HTTP, che response 200 rồi reload/retry cùng public body/signature. B public OPK 34/watermark 41, local private OPK 35/cache keys 5; một public OPK hao ở sender save-abort vẫn còn private local |
| Plaintext/group và thiếu browser tính năng | PASS UI direct/group send/render; thiếu Web Locks/IndexedDB hoặc asset WASM hiện lỗi và chặn E2EE, plaintext composer vẫn hoạt động |
| Node controller/loader/storage/DOM guard riêng | PASS **76 + 14 + 83 assertions**; dùng mock state/API/DOM theo từng helper, không cộng vào browser assertions |

Mất response được mô phỏng sau backend 2xx/commit/publish bằng wrapper HTTP tạm trả 503, không phải TCP disconnect hoặc server crash. Storage failure là native IndexedDB transaction.abort, không chứng minh quota/power-loss PASS. **Chưa chạy B→A, đầy đủ Prompt 5 hoặc ADR #4, browser khác/mobile, eviction/quota/crash và runtime lệch phiên bản thực tế.** Không suy những phần đó từ kết quả trên.

Đã dừng hai harness/consumer/gateway và các Edge processes của demo; xóa riêng fixture DB/Redis và hai profile/helper/script tạm. Kiểm tra độc lập tám UUID users và hai Redis namespaces còn 0 user/key. PostgreSQL/Redis có sẵn vẫn chạy, không reset/TRUNCATE/reseed database. Các script tạm không được giữ như test thường trực; làm lại demo bằng các thao tác bên trên với tài khoản/profile mới.

## Evidence Prompt 5 — hai chiều và bàn giao

Hoàn tất ngày **04/10/2026**, cùng branch/HEAD và phiên bản môi trường nêu trên. Không sửa implementation Prompt 1–4: SHA-256 93 files bảo vệ giữ nguyên. API, crypto, WASM, IndexedDB, Web Locks và chat đang dùng đúng code trong working tree; không có fix implementation cần thực hiện trong lượt này. Đã viết ADR #4 từ nguồn Signal chính thức và đối chiếu metadata với code/schema hiện tại.

| Kiểm tra | Môi trường và kết quả thực tế |
| --- | --- |
| `go test -count=1 ./internal/e2ee ./internal/module/e2ee/service ./internal/module/thread/service ./internal/module/message/service ./internal/module/user/service ./internal/wsticket` | PASS; chạy mới sáu package Go native, không dùng cache |
| `make test`, `make build`, `make wasm`, `go vet ./...` | PASS; `make test` dùng cache sau lượt `-count=1`. Runtime SHA-256 khớp Go compiler; binary/runtime ignored |
| `gofmt -l` trên crypto/bridge/E2EE module và các file thread/message/route liên quan; `node --check` năm JS | PASS, không file cần format trong phạm vi đó. Không đổi query nên không chạy `make sqlc` |
| `node .tmp-e2ee-p5-loader.cjs` | PASS **14 assertions** trên Node: artifact/runtime Go thật và fault thiếu import; mock HTTP 404/bridge error trả lỗi an toàn. Không phải browser hoặc hai phiên bản Go khác nhau |
| Harness Go tạm + `node .tmp-e2ee-p5/browser.cjs` | **40 core assertions đạt**, command exit 1 do helper chờ nút retry sau khi REST đã xác nhận pending; không ghi toàn bộ command PASS. Hai Edge profiles/PostgreSQL/Redis/API/gateway thật |
| `node .tmp-e2ee-p5/continue.cjs`; `node .tmp-e2ee-p5/followup.cjs` | PASS/exit 0: **12 + 11 assertions**, hoàn tất phần còn lại trên cùng fixture; tổng **63 assertions browser đạt** |
| A→B, B→A, 4DH/3DH | PASS: **51 messages**, A gửi 27 (18 4DH/9 3DH), B gửi 24 (20 4DH/4 3DH). Mỗi UUID mới có EK/nonce/key riêng; cả hai chiều có OPK và hết OPK đều decrypt/render |
| Network và PostgreSQL | PASS: ciphertext POST không chứa plaintext/private/message key; content SHA khớp cache, POST và DB. Seq 1–51/last_seq 51, 51 UUID/EK/nonce khác nhau, không dòng chat trùng; hai read markers 51 |
| Reload/own history/older | PASS: checkpoint 46 tin ở cả hai client, latest page + Tin cũ hơn decrypt đủ 46 bằng cached keys; người gửi đọc own messages. Cuối lượt A có 51 cached keys khác nhau; B mất state do case chủ động bên dưới |
| Retry/pending | PASS: che response sau DB commit, reload chưa mở history rồi nút retry dùng nguyên body/UUID, không claim/Seal hoặc thêm row/seq. REST history xác nhận pending exact mà không POST lại. HTTP retry original ở seq 51 trả row cũ; sửa ciphertext/nonce/format đều 409, DB/seq không đổi |
| Realtime/catch-up | PASS: WS hai chiều, event trùng không thêm dòng; gateway fixture dừng rồi REST history vẫn decrypt, gateway mở lại kích hoạt lấy bù. Offline/reconnect đã có browser evidence Prompt 4, giữ nguyên code |
| Tamper/TOFU/thiếu private OPK | PASS trên receive pipeline/UI thật với frame hoặc claim response bị inject: nonce/ciphertext/header/AAD context/hash/signature/pin sai bị chặn; placeholder/lỗi, không plaintext/read/ghi đè cache/xóa OPK. Header có OPK ID nhưng thiếu private OPK/cache báo lỗi, không fallback 3DH |
| Web Lock và mất IndexedDB | PASS: tab hai bị chặn, đóng owner rồi chủ động takeover đọc durable history. Chủ động clear IndexedDB bằng CDP rồi login: missing_keys, chặn E2EE, không tự init/upload/claim hoặc thay IK/SPK |
| Backend prekeys/publish và local abort/regression | Tái sử dụng evidence thực Prompt 2–4 vì code giữ nguyên: concurrent claim, retry upload không hồi sinh OPK, mismatch rollback/watermark, XADD lỗi thật sau commit; native IndexedDB abort giữ OPK/cache/read, upload/refill retry, logout/scope, plaintext/group. Không ghi là chạy mới tất cả trong Prompt 5 |

Core helper dừng ở bước retry vì mở REST history đã xác nhận pending đúng contract. Continuation dùng cùng fixture, reload chưa mở thread rồi kiểm chứng nút retry; không reset dữ liệu hoặc tính bước chưa chạy là PASS. Các fault frame/claim/pin/cache được inject để đi qua app/Go-WASM/UI thật; không khẳng định backend đã sinh ciphertext sai. Cuối lượt **56 POST attempts = 51 mới + 2 retry + 3 conflict 409**. Hai lượt claim kiểm tra lỗi không POST message làm hao OPK đúng thiết kế: **53 claims/51 messages**, public OPK cuối bằng 0 ở cả hai phía, watermark vẫn 21/SPK 1.

**CHƯA CHẠY:** quota/eviction tự nhiên, power loss/process crash, runtime Go thực tế lệch phiên bản, browser khác/mobile/Unix và race detector. Native transaction.abort của Prompt 4 và clear IndexedDB có chủ đích của Prompt 5 chỉ chứng minh các fault đó; không suy quota/crash PASS. Lỗi response được mô phỏng sau HTTP thành công/commit, không phải TCP disconnect hoặc crash backend. Mentor chưa review sơ đồ. Các giới hạn bảo mật và phạm vi được giải thích trong ADR #4; không có Double Ratchet, rotation, backup, multi-device hoặc E2EE nhóm.

Cleanup Prompt 5 đã hoàn tất: kiểm tra độc lập còn **0 fixture users/threads, 0 Redis namespace/ticket keys, 0 owned harness/Edge processes**. Đã đóng API/gateway/consumer/Hub/pool, kiểm tra đường dẫn rồi xóa `.tmp-e2ee-p5`, hai profiles, scripts/exe/log/private fixture JSON; PostgreSQL/Redis có sẵn và dữ liệu khác giữ nguyên. Helpers/profile/fixture tạm dùng để tạo evidence, không phải lệnh demo còn tồn tại trong repo; tái hiện bằng các bước web ở đầu tài liệu với tài khoản/profile mới.

## Sửa initial catch-up — bàn giao 05/10/2026

Branch `feat/e2ee`, HEAD `0bff8cf436fa1f3855ce1b675ce11b808ec0f370`, đúng baseline của tài liệu catch-up; giữ các thay đổi tài liệu đã có. Trước sửa, cache mới đặt `syncedSeq` bằng seq cao nhất của trang đầu, nên đã đọc đến 5/mới nhất 50 chỉ tự tải 21–50 và có thể xác nhận đọc 50 khi còn thiếu 6–20.

`web/app.js` hiện chụp `last_read_seq` từ summary trước HTTP, dùng `fetchThroughBoundary` để lấy đến boundary/hết cursor, chỉ đặt `messagesLoaded`/`syncedSeq` khi lượt thành công. Dùng các state hiện có, không thêm state đồng bộ mới. Lỗi giữ flag chưa hoàn tất, cache đã merge được khử trùng khi mở lại thread/reconnect thử lại. Cursor của trang cũ nhất được giữ cho nút Tin cũ hơn; nút này chờ initial sync hoàn tất. Read không vượt `syncedSeq`, nên realtime có gap chưa lấy bù cũng không được xác nhận sớm. Initial/catch-up/older bỏ callback khi phiên/cache/membership epoch thay đổi.

README, sơ đồ riêng và đoạn hướng dẫn demo đã khớp hành vi mới. Hai đoạn cũ trong ADR #2/request-flow cho phép đọc hết từ trang đầu 30 tin được chỉnh đúng điều kiện initial sync; các quyết định và evidence lịch sử khác giữ nguyên. Phạm vi đề xuất cho mentor nằm ở đầu tài liệu, chưa ghi đã được duyệt.

| Lệnh / case | Môi trường và kết quả thực tế |
| --- | --- |
| `node --check web/app.js`; `node --check web/realtime-core.js` | PASS syntax trên Node `v24.11.1` |
| `make test`; `make build` | PASS trên Go `go1.27.1 windows/amd64`; sáu package test dùng cache, không phải browser/DB integration |
| Helper `.tmp-initial-catchup-check.cjs --baseline` trước patch | Tái hiện lỗi bằng 3 assertions: chỉ 21–50, một request, read có thể tới 50; đây là xác nhận bug, không phải behavior mong muốn |
| `node .tmp-initial-catchup-check.cjs` sau patch | PASS/exit 0, **33 assertions tập trung**; chạy app/realtime implementation thật trong Node VM, DOM/HTTP và kết quả decrypt/local commit giả lập |
| Direct boundary 5/latest 50/page size 30 | PASS trong Node: hai trang, đủ 6–50 và phần cũ lấy dư, UUID không trùng, render tăng dần; trang thứ hai đang chờ chưa hoàn tất sync/read |
| Ít hơn 30 tin bỏ lỡ/không tin mới/cache đã tải | PASS trong Node: một trang, đổi lại thread không tải toàn bộ lần nữa. Boundary 0 đi hết cursor, kể cả lịch sử rỗng |
| Group có khoảng không được xem | PASS trong Node với 50 messages được phép qua hai trang, gap seq 11–30 bị loại; dừng bằng cursor/boundary, không cố lấp gap |
| Page tiếp theo lỗi và retry | PASS trong Node: `messagesLoaded=false`, không read, request guard được nhả; trigger sync hiện có lấy đủ và gộp partial cache |
| Cursor tải cũ | PASS trong Node: boundary 55/latest 150 lấy bốn trang tới cursor 31; Tin cũ hơn chỉ lấy 1–30 bằng cursor 31, không lấy lại trang mới nhất |
| E2EE read gate | PASS ở app với kết quả receive/commit giả lập: ciphertext pending, chưa render, tab non-owner hoặc ngoài cuối viewport không read; kết quả ready/committed/rendered mới được tính |
| Realtime/stale callbacks | PASS trong Node: duplicate lúc initial fetch không trùng; seq 65 tới sau REST snapshot 50 bị chặn read trong lúc gap 51–64 pending/lỗi, sync lại mới tới 65. Logout/chuyển thread/membership không render/read nhầm; cached catch-up và older response của membership cũ không merge |
| Diff/hash/links/test scope | PASS kiểm tra tĩnh; 99 files bảo vệ giữ nguyên, crypto/API/Go-WASM/IndexedDB/Web Locks/schema/dependencies/contract và nguồn prompt không đổi; vẫn chỉ sáu permanent test files |

Hai case stale membership của cached catch-up và older đã tái hiện thất bại trước guard mới rồi đạt trong suite cuối; không ghi các lượt tái hiện bug là PASS behavior đúng. Helper đã xóa sau kiểm chứng, không giữ thành test thường trực. Không tạo fixture DB/profile hoặc sửa dữ liệu người dùng trong lượt này.

**CHƯA CHẠY cho sửa đổi này:** browser/DB/Redis/gateway thật, native WASM/IndexedDB/Web Locks, network disconnect và storage failure thật. Các browser/DB results Prompt 4–5 ở trên là evidence lịch sử, không biến thành PASS mới cho initial catch-up. Không chạy lại `make wasm` vì crypto/bridge/build không đổi. Không migration/reset/TRUNCATE, thêm endpoint/polling/timer retry hoặc mở rộng phạm vi E2EE.
