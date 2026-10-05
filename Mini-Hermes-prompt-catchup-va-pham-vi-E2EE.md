# Mini-Hermes: sửa lấy bù và chốt phạm vi E2EE

Sao chép cả phần A và B cho Codex trong repository `D:\VinAI\Project\chat-api`, hoặc gửi nguyên file này. Phần A là chỉ dẫn thực hiện; phần B là nội dung đề xuất để trình bày với mentor và đưa vào tài liệu demo. Công việc gồm sửa initial catch-up và ghi rõ phạm vi demo; chưa làm các mục đơn giản hóa pending, gom contract hoặc integration test tuần 6.

## A. Prompt cho Codex

Bạn đang làm việc trong Mini-Hermes. Hãy thực hiện đúng hai việc dưới đây bằng thay đổi nhỏ nhất, dùng cơ chế đang có. Mục tiêu là đáp ứng yêu cầu offline quay lại nhận đủ tin, đồng thời giữ phạm vi E2EE dễ hiểu cho bài thực tập.

### 1. Đọc trạng thái hiện tại

- Đọc `AGENTS.md`, kiểm tra branch/HEAD và working tree. Baseline được review là `feat/e2ee`, commit `0bff8cf436fa1f3855ce1b675ce11b808ec0f370`. Nếu HEAD khác, ghi rõ và đối chiếu code hiện tại; không checkout/reset về baseline. Giữ các thay đổi có sẵn của người dùng.
- Đọc các phần liên quan trong `web/app.js`: cache, summary/last_read_seq, `loadInitialHistory`, `catchUpConversation`, merge/render, read marker và tải tin cũ. Đọc `web/realtime-core.js`, README và `docs/e2ee-demo.md`.
- Để ghi phạm vi E2EE chính xác, đối chiếu ownership trong `web/e2ee.js` và phần II.1–2 của `docs/e2ee-contract.md`. Không sửa crypto hoặc state E2EE.
- Nếu lỗi dưới đây đã được sửa ở HEAD mới, kiểm chứng rồi báo rõ; không tạo thay đổi chỉ để có diff.

### 2. Việc thứ nhất: sửa lấy bù khi mở lại trang

Hiện tượng đã xác nhận ở baseline:

- B đã đọc đến seq 5, đóng/reload trang; A gửi đến seq 50.
- Trang đầu trả 30 tin, seq 50…21.
- `loadInitialHistory` đặt `syncedSeq=50` ngay từ trang này; target cũng bằng 50 nên không tự lấy bù seq 6…20.
- Tin còn trong PostgreSQL nhưng phải bấm “Tin cũ hơn”. Read marker còn có thể tiến đến 50 trước khi phần bỏ lỡ được tải đủ.

Hành vi cần đạt:

1. Khi cache lịch sử của thread chưa được tải, chụp `last_read_seq` từ summary hiện có làm ranh giới lấy bù bảo thủ. Đây là mốc đọc, không phải mốc sync chính xác: có thể tải dư tin đã nhận nhưng chưa đọc, rồi khử trùng bằng UUID/seq.
2. Dùng lại `fetchThroughBoundary` và cursor `before_seq`/`next_cursor`. Tải trang mới nhất rồi lùi đến ranh giới đã chụp hoặc hết lịch sử được API cho phép. Nếu ranh giới là 0 thì dừng khi API hết cursor. Không đặt ranh giới theo seq lớn nhất của trang mới nhất.
3. Chỉ cập nhật trạng thái initial sync hoàn tất sau khi lượt lấy bù thành công. Khi đang tải hoặc lỗi giữa chừng, không cho read marker vượt phần đồng bộ chưa hoàn tất. Tận dụng state hiện có; chỉ thêm state nếu thật sự cần, giải thích lý do.
4. Gộp UUID/seq và hiển thị tăng dần như hiện tại. Giữ cursor tải tin cũ phù hợp với trang cũ nhất đã lấy, để “Tin cũ hơn” tiếp tục hoạt động. Tận dụng page đã lấy nếu đơn giản; không tạo hai vòng tải toàn bộ cùng một khoảng.
5. Nhóm có thể có khoảng seq không được xem khi thành viên vắng mặt. Dừng theo cursor/ranh giới và quyền API, không lặp vô hạn để lấp mọi số seq.
6. Giữ guard session/thread/membership hiện có. Logout, chuyển thread hoặc membership thay đổi không được để callback cũ render/đánh dấu đọc sai. Tin realtime tới trong lúc lấy bù không được làm mất phần lịch sử đang cần tải.
7. Giữ điều kiện read E2EE: decrypt thành công, local commit và render, đúng tab owner và viewport. Lấy bù đủ ciphertext không tự chứng minh người dùng đã đọc.

Chỉ sửa phần web liên quan. Không thêm endpoint, bảng, migration, dependency, polling, timer retry mới, sync protocol, lớp abstraction hoặc hệ thống lưu cursor riêng. Không đổi schema/API/crypto/AAD/envelope/Go-WASM/IndexedDB/Web Locks. Không thay quy ước read marker thành danh sách tin đã xem rời rạc. Luồng reconnect đang hoạt động phải được giữ.

### 3. Việc thứ hai: ghi rõ phạm vi demo E2EE

Bổ sung một đoạn ngắn ở đầu `docs/e2ee-demo.md`, dùng nội dung phần B bên dưới. Nêu đây là phạm vi đề xuất để mentor xem, không ghi mentor đã duyệt.

Phạm vi phải phản ánh implementation hiện có:

- E2EE cho direct A→B và B→A trên web. Mỗi tài khoản demo dùng một browser profile cố định và một tab E2EE hoạt động.
- Web Lock chọn owner trong cùng origin/profile. Tab thứ hai không decrypt/send/read E2EE cho đến khi tiếp quản; không thêm hỗ trợ nhiều owner.
- Plaintext direct/group vẫn dùng luồng hiện có; không chuyển thread plaintext cũ thành E2EE.
- Private keys ở client; server lưu public bundle và ciphertext/envelope. REST gửi, WebSocket nhận, REST lấy bù.
- Mỗi tin mới dùng lượt X3DH mới; retry giữ nguyên UUID/payload. Giữ 20 OPK ban đầu và fallback 3DH khi hết OPK như code hiện tại.
- Chưa làm Double Ratchet, group E2EE, multi-device, key backup/rotation hoặc đồng bộ khóa giữa tab.
- Phân biệt nhiều connection của recipient được gateway fan-out với nhiều tab E2EE cùng giải mã. Text của sender hiện không được echo sang các connection khác của sender; không mô tả là đã đồng bộ đầy đủ các tab cùng tài khoản.

Sơ đồ để mentor xem đã có ở phần II.2 của `docs/e2ee-contract.md`: đăng ký bundle → claim bundle → X3DH/encrypt ở client → POST ciphertext → WebSocket/REST → decrypt/local commit/render. Thêm link tới sơ đồ hiện có; không sao chép hoặc tạo sơ đồ mới trong lượt này.

Nếu README mô tả hành vi initial catch-up/read trái với sửa đổi ở mục 2, chỉnh đúng đoạn đó. Không viết lại toàn bộ tài liệu; không gom contract, rút ADR hoặc sửa các nút pending trong lượt này. Evidence lịch sử giữ nguyên; không biến các kiểm tra cũ thành PASS mới.

### 4. Kiểm chứng vừa đủ

Chạy syntax JS cho file đã sửa, kiểm tra diff và các lệnh mà AGENTS yêu cầu khi phù hợp. Dùng kiểm tra tạm hoặc cách sẵn có để xác minh:

1. Direct: last_read_seq=5, last_seq=50, page size 30; tự có đủ seq 6–50, không trùng, thứ tự đúng; read không tiến qua initial sync chưa hoàn tất.
2. Ít hơn 30 tin bỏ lỡ và trường hợp không có tin mới: tải ban đầu bình thường, chuyển lại thread đã cache không tải toàn bộ lần nữa.
3. Group: lịch sử có khoảng seq bị loại theo membership; lấy đủ phần được phép và dừng đúng cursor.
4. Lỗi page tiếp theo: không tuyên bố sync hoàn tất hoặc đọc hết; thử lại bằng trigger hiện có có thể lấy đủ.
5. E2EE: ciphertext chưa decrypt/commit/render không làm tăng read marker; tab không owner vẫn bị chặn như trước.
6. Tin realtime tới trong lúc tải và callback sau logout/chuyển thread không làm trùng tin hoặc cập nhật nhầm UI/read.

Không thêm test thường trực ngoài quy định hiện tại. Nếu dùng helper tạm, dọn sau khi ghi kết quả. Phân biệt kiểm tra Node/mock với browser/DB thật. Chưa chạy phần nào thì ghi rõ, không khẳng định PASS. Không tạo một bộ hàng trăm assertions chỉ để tăng số lượng.

Không reset/TRUNCATE DB, chạy migration, thay branch, commit, push hoặc merge. Nếu cần kiểm chứng thật, chỉ dùng fixture riêng và dọn đúng dữ liệu do lượt này tạo; không động dữ liệu người dùng.

### 5. Báo cáo và dừng

Trả: branch/HEAD; nguyên nhân; luồng trước/sau; file đã sửa và vì sao; kiểm tra đã chạy/giới hạn; đoạn phạm vi E2EE cho mentor. Chỉ cập nhật tài liệu bị ảnh hưởng và đoạn phạm vi demo đã yêu cầu. Hoàn tất hai việc rồi dừng; không tự triển khai các khuyến nghị khác.

## B. Nội dung phạm vi E2EE để trình bày với mentor

Phạm vi demo đề xuất là chat direct E2EE hai chiều trên web, mỗi tài khoản dùng một browser profile cố định và một tab E2EE hoạt động. Giới hạn này giúp private keys và trạng thái gửi/nhận có một nơi quản lý. Tab thứ hai phải chờ tiếp quản quyền sở hữu state.

Client sinh khóa, upload public bundle, claim bundle của người nhận, tính secret và mã hóa trước khi gửi. Server lưu/truyền ciphertext cùng metadata; người nhận giải mã bằng khóa local. Gửi bằng REST, nhận qua WebSocket và lấy bù lịch sử bằng REST. Mỗi tin mới dùng một lượt X3DH; retry dùng lại nguyên UUID và payload.

Demo có 20 OPK ban đầu; khi hết OPK, code dùng 3DH với giới hạn đã ghi trong ADR #4. Chưa mở rộng sang Double Ratchet, E2EE nhóm, nhiều thiết bị, backup/rotation hay đồng bộ khóa giữa tab. Chat nhóm và direct plaintext tiếp tục hoạt động theo luồng hiện có.

Gateway có fan-out đến nhiều kết nối của recipient, nhưng điều đó không có nghĩa nhiều tab E2EE cùng giải mã được. Luồng hiện tại cũng không echo text sang các tab khác của sender. Phạm vi trên cần mentor xác nhận trước khi dùng làm tiêu chí nghiệm thu; chưa có bằng chứng mentor đã duyệt sơ đồ.
