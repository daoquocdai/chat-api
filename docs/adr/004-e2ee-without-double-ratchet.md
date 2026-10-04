# ADR 004: X3DH độc lập mỗi message, chưa dùng Double Ratchet

Ngày: 04/10/2026. Trạng thái: chấp nhận cho phạm vi demo tuần 5. Hợp đồng: [e2ee-contract.md](../e2ee-contract.md), phần II; cách chạy và evidence: [e2ee-demo.md](../e2ee-demo.md).

## Bối cảnh

Mini-Hermes cần giữ nội dung direct E2EE bí mật với bên chỉ có dữ liệu API/PostgreSQL/Redis/gateway, trong điều kiện mã client, RNG và private state chưa bị kiểm soát. Giao diện hiện có gửi bằng REST, nhận WebSocket và đọc/lấy bù lịch sử bằng REST. Receiver có thể offline sau khi đăng ký public bundle. Thread plaintext và group giữ nghiệp vụ cũ.

X3DH thiết lập secret bất đồng bộ bằng IK của hai bên, SPK đã ký của recipient và EK mới của sender; có OPK thì thêm DH thứ tư. Sender kiểm tra chữ ký trước khi tính key. Recipient dùng private prekeys tương ứng và header để tính cùng secret. Đây là bước thỏa thuận khóa; quản lý khóa cho nhiều tin còn cần quyết định riêng. [X3DH §3](https://signal.org/docs/specifications/x3dh/#the-x3dh-protocol).

Trong `internal/e2ee/crypto.go`, `Seal` tạo EK bằng X25519 và nonce 12 byte từ crypto/rand, kết hợp DH bằng HKDF-SHA-256 thành key 32 byte, rồi dùng AES-256-GCM. `Open` tính DH theo vai recipient; `DecryptWithKey` chỉ đọc tin đã cache. AAD ràng buộc hai public IK, thread/message/sender/recipient UUID và prekey header theo byte layout đã chốt; không bind seq/thời điểm do server cấp. Không thay primitive, encoding, envelope hoặc chữ ký hàm trong ADR này.

## Lựa chọn và quyết định

| Lựa chọn | Hệ quả trong phạm vi dự án |
| --- | --- |
| Duy trì một secret để gửi nhiều tin hai chiều | Cần thiết kế riêng chống replay/key reuse và quản lý nonce/state; không phải hợp đồng đã chốt |
| X3DH mở phiên rồi Double Ratchet | Cần root/send/receive chains, DH ratchet, counters, skipped keys và transaction state khi nhận khác thứ tự |
| X3DH mới cho mỗi message | Dùng lại public prekeys API và controller đã có; mỗi tin là một lượt độc lập, tốn claim/OPK và giữ key local để đọc lịch sử |

Chọn **X3DH độc lập cho mỗi message mới**. B trả lời phải claim bundle A, dùng IK của B cùng EK/nonce/UUID mới; không dùng secret nhận ở tin A→B. Retry của cùng tin giữ nguyên UUID/body/envelope/key/nonce đã commit, không gọi claim hoặc Seal lần nữa. `web/e2ee.js` dùng sender/recipient từ actor và thread summary, không hardcode chiều gửi. Có OPK chạy 4DH, claim trả null thì chạy 3DH; header có OPK ID mà thiếu private key phải lỗi, không tự bỏ DH4.

Double Ratchet thêm symmetric chains tạo key từng tin và DH mới trộn vào root chain. Bảo vệ tin cũ cần xóa khóa cũ; phục hồi trước người nghe lén thụ động cần entropy DH mới không bị lấy. State/counters/skipped keys để xử lý tin khác thứ tự làm phạm vi triển khai lớn hơn. Tuần 5 chốt demo lượt độc lập và key cache, nên chưa thêm các cấu trúc đó. [Double Ratchet §2–3](https://signal.org/docs/specifications/doubleratchet/#overview).

## Điều kiện bảo mật và đánh đổi

| Tình huống | Kết luận có điều kiện cho thiết kế hiện tại |
| --- | --- |
| Chỉ lấy DB/stream hoặc transcript, client/key/RNG còn tin cậy | Ciphertext không có key cần để decrypt; server vẫn thấy metadata. Kết luận này không áp dụng khi mã trang hoặc endpoint client bị kiểm soát |
| Có OPK, về sau chỉ lộ recipient IK/SPK | Secret cũ có thể được bảo vệ nếu OPK private và EK private của lượt đó không còn lấy được, message key không bị lấy và DH/KDF còn an toàn |
| Không OPK, lộ đúng recipient IK/SPK của lượt cũ | Transcript đủ cho attacker tính ba DH và key cũ. SPK bất biến trong demo kéo dài cửa sổ ảnh hưởng; nonce/EK mới không khắc phục việc recipient private của cả ba DH đã lộ |
| Lộ toàn bộ profile IndexedDB/RAM của một phía | Message keys được giữ cho lịch sử cho phép decrypt các tin phía đó đã cache, kể cả 4DH đã consume OPK. Có thể lộ private prekeys chưa dùng và pending; không tuyên bố forward secrecy cho cached history khi toàn state bị lấy |
| Đã compromise rồi client gửi thêm tin | Lượt mới tránh dùng lại key nhận, nhưng không tự thay/xóa IK/SPK đã lộ hoặc xử lý attacker còn kiểm soát client. OPK mới chưa bị lấy có thể bảo vệ một số lượt trước attacker chỉ nghe lén thụ động; không loại trừ impersonation/KCI dựa trên IK/SPK đã lộ. Đó chưa phải quy trình phục hồi của một phiên ratchet |

Phân tích OPK/3DH ở trên đối chiếu [X3DH §4.6–4.7](https://signal.org/docs/specifications/x3dh/#key-compromise). Không có ratchet không đồng nghĩa mất mọi khả năng forward secrecy của X3DH. Nó khác bảo vệ do cập nhật/xóa chain keys liên tục; ở đây key lịch sử cố ý còn trong profile. `deriveKey` clear một số buffer DH/KDF nhưng không chứng minh xóa mọi bản sao key khỏi Go/JavaScript/browser storage. DELETE public OPK và bỏ private OPK khỏi record là xóa logic, không phải evidence xóa vật lý khỏi RAM/flash.

Double Ratchet cũng không chữa được mã độc còn chạy, RNG bị điều khiển hoặc attacker duy trì MITM chủ động. Khả năng phục hồi có điều kiện về entropy mới và attacker mất khả năng lấy nó; secure deletion còn phụ thuộc nền tảng. Vì vậy **fresh X3DH mỗi tin không được mô tả là đầy đủ post-compromise security của Signal**. [Double Ratchet §8.1–8.2](https://signal.org/docs/specifications/doubleratchet/#security-considerations).

### OPK và độ sẵn sàng

`internal/module/e2ee/repository/postgres.go` khóa recipient user trong transaction, consume OPK bằng DELETE RETURNING và chỉ trả bundle sau commit. Các claim đã commit không nhận cùng OPK; response mất hoặc sender bỏ lượt có thể hao OPK. Upload retry dùng watermark ID cao nhất, không hồi sinh ID đã consume. Không có reservation/hoàn trả; IK/SPK và signature bất biến, refill chỉ thêm OPK ID mới.

Ở receiver, `web/e2ee.js` Open xong mới save message key/pin và bỏ private OPK bằng cùng put/transaction IndexedDB; `web/e2ee-state.js` chờ oncomplete. AEAD/local commit lỗi giữ state cũ và OPK. Cho phép 3DH tăng khả năng gửi khi OPK cạn, với giới hạn compromise nêu ở bảng. Peer hợp lệ hoặc server từ chối OPK có thể làm cạn/giảm lợi ích của OPK; claim có authorization theo thread nhưng không có cơ chế rate limit/reservation mới trong bước này.

### Replay, retry và xác thực

UUID unique/retry payload exact ở PostgreSQL, hash cached content và AAD context giúp từ chối payload khác hoặc bản sao trong phạm vi state ứng dụng còn giữ. Chúng không ngăn mọi replay của initial X3DH, nhất là khi không OPK và state nhận đã mất. X3DH §4.3 yêu cầu randomize key trước khi recipient gửi sau initial exchange; B→A ở đây thực hiện lượt mới trong vai sender. Không dùng secret A→B hoặc bản dẫn xuất chỉ đổi nhãn HKDF của secret đó để reply; không gọi UUID/hash là ratchet chống replay đầy đủ. [X3DH §4.2–4.3](https://signal.org/docs/specifications/x3dh/#replay-and-key-reuse).

Chữ ký SPK chỉ xác thực SPK với IK được bundle cung cấp. TOFU pin IK phát hiện thay đổi về sau nếu pin chưa bị sửa; lần gặp đầu server/MITM có thể đưa IK khác cùng SPK/signature hợp lệ. Fingerprint cần đối chiếu UUID và IK qua kênh đã xác thực; xem fingerprint trên cùng trang bị compromise không tạo kênh độc lập. AAD bind account/thread/context không tự xác nhận người thật sở hữu account. [X3DH §4.1 và §4.8](https://signal.org/docs/specifications/x3dh/#authentication).

**Chữ ký là biến thể libsignal** `go.mau.fi/libsignal v0.2.2`, sign bit nằm trong signature, ký `0x05 || SPK_public32` như contract. Đây không phải cam kết canonical XEdDSA-2016 wire encoding hoặc tương thích đầy đủ với Signal. Không có Signal session/Double Ratchet, multi-device hay group E2EE trong implementation này.

## Metadata và ranh giới client web

| Dữ liệu server nhìn thấy | Đối chiếu code/schema hiện tại |
| --- | --- |
| Username/user UUID/password hash; public IK và watermark | users/schema, user API/auth và `db/queries/prekeys.sql`; không có private IK/SK |
| SPK/OPK public, signature, ID/kind/thời điểm | prekeys/schema và module/e2ee; không upload private material |
| Thread UUID/mode/kind/creator và các participant | threads/participants sau migration use_external_message_id; cặp direct được suy từ participants, không có hai cột direct pair cũ |
| Message UUID/sender/seq/thời điểm/format/content bytes | messages/schema và module/message repository; `content` là nguyên envelope, `metadata` không phải chỗ cất key |
| Envelope recipient/IK sender/EK public/prekey IDs/nonce/ciphertext | `internal/e2ee/types.go` và ParseEnvelope/header validation; các trường này không được che bởi AES-GCM |
| Recipient snapshot, read marker/unread và luồng gửi/nhận | event/publisher/Redis, gateway fan-out, participants.last_read_seq/summary; XACK không chứng minh recipient đã đọc |
| HTTP/WS endpoint, origin/thời gian và thông tin kết nối | router/Gin/gateway/ticket; IP/query vé có thể thấy tại hạ tầng nếu ghi log, không khẳng định có cột IP trong DB |

Trong điều kiện client/code tin cậy, E2EE chỉ bảo vệ nội dung; không che ai liên hệ ai, độ dài hoặc nhịp trao đổi. HTTPS/WSS cần cho mạng thực, đặc biệt bảo vệ đăng nhập/JWT, public bundle và mã tải xuống; TLS không bảo vệ khỏi chính bên được quyền thay mã trang.

Private keys/message keys nằm trong IndexedDB/RAM. Nếu profile bị lấy, XSS/extension đủ quyền hoặc JavaScript/WASM/runtime phục vụ từ server bị thay, attacker có thể đọc key, gọi bridge hoặc lấy plaintext. **WASM không cách ly bí mật khỏi mã độc trong cùng trang**; Base64 không mã hóa storage. Cùng compiler/runtime là yêu cầu tương thích, không phải cơ chế chứng thực mã client độc lập với server. `syscall/js` bridge chỉ làm crypto; JavaScript vẫn quản lý state/HTTP/DOM và có quyền với key DTO.

Web Locks chỉ chọn owner giữa tab cùng origin/profile và Promise queue serialize state. Nó không điều phối khác profile/thiết bị, không ngăn mã độc hoặc cấp danh tính peer. IndexedDB transaction giữ các cập nhật local liên quan cùng nhau khi commit/abort, không bảo đảm chống eviction/mất điện hay xóa vật lý key. Mất state có thể làm mất lịch sử; UI báo missing_keys, không tự thay IK/SPK. Không có rotation, backup/export/import, multi-device, E2EE nhóm hoặc recovery trong phạm vi này.

## Kiểm chứng và phạm vi kết luận

Native tests kiểm tra primitive/format/AAD/signature và service rules; browser demo kiểm tra WASM/IndexedDB/Web Locks/HTTP/realtime trên môi trường cụ thể. PostgreSQL SELECT ciphertext không tự chứng minh crypto hoặc endpoint an toàn. Evidence từng môi trường, fault injection và phần chưa chạy nằm cuối contract và demo; chỉ các kiểm chứng đã thực hiện được ghi PASS. ADR không phải chứng nhận bảo mật hoặc bằng chứng secure deletion/PCS.
