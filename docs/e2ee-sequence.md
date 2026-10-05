# Mini-Hermes — Tuần 5: sequence diagram X3DH

**Trạng thái:** sơ đồ và phạm vi demo đề xuất để người hướng dẫn xem xét; chưa có xác nhận đã được duyệt.

Hai sơ đồ liên kết mô tả đăng ký public bundle → claim bundle của người nhận → tính shared secret tại client → mã hóa → gửi ciphertext → người nhận giải mã. Private keys và shared secret nằm ở client; server lưu public keys, ciphertext và metadata.

Nguồn sơ đồ: [Mini-Hermes-tuan5-sequence-X3DH.md](../Mini-Hermes-tuan5-sequence-X3DH.md). Đối chiếu với [contract](e2ee-contract.md), [hướng dẫn demo](e2ee-demo.md) và [ADR #4](adr/004-e2ee-without-double-ratchet.md). Yêu cầu lấy bù và phạm vi demo nằm trong [tài liệu catch-up](../Mini-Hermes-prompt-catchup-va-pham-vi-E2EE.md).

## 1. Phạm vi và ký hiệu

E2EE dành cho direct A→B và B→A trên web. Mỗi tài khoản demo dùng một browser profile cố định và một tab E2EE hoạt động. Web Locks chọn tab owner trong cùng origin/profile; tab thứ hai chờ tiếp quản, chưa được decrypt/send/read E2EE. Crypto chạy trong Go/WASM; JavaScript quản lý HTTP, WebSocket, IndexedDB và giao diện.

Chat nhóm và direct plaintext dùng luồng hiện có; không đổi thread plaintext cũ sang E2EE. Chưa triển khai Double Ratchet, E2EE nhóm, multi-device, backup/rotation hoặc đồng bộ khóa giữa tab.

| Ký hiệu | Ý nghĩa | Vị trí lưu |
| --- | --- | --- |
| IK | Identity key — cặp khóa định danh của tài khoản | Private ở client; public ở server |
| SPK | Signed prekey — public key được ký bằng IK private | Private ở client; public, ID và chữ ký ở server |
| OPK | One-time prekey — cặp khóa dùng cho một lượt X3DH | Private ở client; public được server cấp một lần |
| EK | Ephemeral key — cặp khóa mới của sender cho mỗi lượt | Private dùng tại client gửi; public đi trong envelope |
| SK | Shared secret / message key — khóa 32 byte từ DH/HKDF | Tính độc lập tại hai client, cache local để đọc lịch sử; không gửi server |
| AAD | Associated data — ngữ cảnh được xác thực cùng ciphertext | Tạo từ public IK, context và header của tin |

Upload bundle chứa IK public, SPK public kèm ID/chữ ký và một batch OPK public. Claim trả tối đa một OPK. Demo khởi tạo 20 OPK mỗi tài khoản; giữ IK/SPK bất biến, refill chỉ thêm OPK ID mới. Private state phân vùng theo API URL và user UUID trong IndexedDB.

## 2. Đăng ký public bundle → lấy bundle

A và B đã đăng nhập, WASM sẵn sàng và mỗi người có tab owner. Sinh khóa chỉ ở lần khởi tạo chủ động; reload khôi phục bộ khóa đã lưu. Sau khi cả hai đăng ký, A tạo hoặc mở direct thread `encryption_mode=e2ee`.

```mermaid
sequenceDiagram
    participant A as Web A + Go/WASM
    participant API as chat-api
    participant DB as PostgreSQL
    participant B as Web B + Go/WASM

    Note over A,B: Mỗi client có Web Lock owner và IndexedDB riêng
    par A đăng ký
        A->>A: generateKeyPair cho IK, SPK, 20 OPK; signPrekey
        A->>A: Commit private keys, signature và pending_upload vào IndexedDB
        A->>API: POST /e2ee/prekeys chỉ public bundle
        API->>DB: Transaction khóa user A và lưu public keys
        DB-->>API: COMMIT thành công
        API-->>A: Xác nhận sau commit
        A->>A: Commit xác nhận đăng ký, bỏ pending_upload
    and B đăng ký
        B->>B: generateKeyPair cho IK, SPK, 20 OPK; signPrekey
        B->>B: Commit private keys, signature và pending_upload vào IndexedDB
        B->>API: POST /e2ee/prekeys chỉ public bundle
        API->>DB: Transaction khóa user B và lưu public keys
        DB-->>API: COMMIT thành công
        API-->>B: Xác nhận sau commit
        B->>B: Commit xác nhận đăng ký, bỏ pending_upload
    end
    Note over B: B có thể offline sau đăng ký
    A->>API: POST /threads/direct với peer_id=B, encryption_mode=e2ee
    API->>DB: Khóa hai user theo thứ tự ổn định; kiểm tra IK/SPK cho thread mới
    API->>DB: Tạo hoặc lấy direct của cặp user, không đổi mode thread cũ
    API-->>A: Thread ID và mode thực tế
    A->>A: Tạo message UUID và context cho lần gửi mới
    A->>API: POST /e2ee/bundles/:user_id/claim với user_id=B, body thread_id
    rect rgb(235, 245, 255)
        API->>DB: BEGIN; kiểm tra direct E2EE và hai participant active
        API->>DB: Khóa user B; đọc IK/SPK, DELETE RETURNING OPK nhỏ nhất
        DB-->>API: Public bundle với một OPK hoặc null
        API->>DB: COMMIT
        DB-->>API: Commit thành công
    end
    API-->>A: Bundle public sau commit
    Note over A: Bundle và context là đầu vào sơ đồ 3
```

Các API nghiệp vụ dùng JWT; owner của upload lấy từ JWT actor, không từ body. `B` là UUID tài khoản peer. Claim chỉ hợp lệ khi actor và peer là hai participant active của direct E2EE thread. Thread cũ có mode khác trả 409, không tạo thêm direct cho cùng cặp hoặc tự chuyển mode.

Public OPK bị consume trong transaction claim để các claim đã commit không nhận cùng khóa. Private OPK tương ứng vẫn ở B, chờ giải mã tin sử dụng nó. Response claim mất hoặc sender bỏ lượt có thể làm hao OPK; không có reservation/hoàn trả. Hết public OPK thì response có `one_time_prekey: null`, lượt mới dùng 3DH.

Upload retry dùng nguyên payload/chữ ký đã lưu; không thay IK/SPK hoặc hồi sinh OPK đã consume. `last_prekey_id` là ID cao nhất đã xác nhận, không phải số khóa còn lại.

## 3. Tính secret → mã hóa → gửi ciphertext → giải mã

Đầu vào A là bundle và context từ sơ đồ 2. Cột **Phát realtime** gộp Redis Stream và ws-gateway cho dễ trình bày; implementation vẫn có hai thành phần riêng: API XADD, gateway đọc Stream và phát WebSocket. Server không mã hóa/giải mã.

```mermaid
sequenceDiagram
    participant A as Web A + Go/WASM
    participant API as chat-api
    participant DB as PostgreSQL
    participant RT as Redis Stream + ws-gateway
    participant B as Web B + Go/WASM

    A->>A: verifyBundle và kiểm tra identity pin
    Note over A: Chữ ký sai hoặc identity đã pin bị đổi thì dừng
    A->>A: seal sinh EK mới, tính DH và HKDF ra SK
    A->>A: Tạo AAD, nonce mới; AES-256-GCM mã hóa plaintext
    A->>A: Commit key, context/hash/pin và nguyên pending POST body vào IndexedDB
    A->>API: POST /threads/:id/messages với message_id, e2ee_v1 và content string
    API->>DB: Transaction khóa thread, kiểm tra membership và retry UUID
    API->>DB: Tin mới kiểm tra mode/header, cấp seq và insert ciphertext atomically
    DB-->>API: Message đã COMMIT
    API->>RT: XADD message.created chứa ciphertext opaque
    Note over API,RT: Publish lỗi sau commit có thể trả 503, sender giữ pending để retry
    API-->>A: Response message sau publish thành công
    A->>A: Response khớp thì commit bỏ pending
    A->>A: decryptWithKey từ cache và render own message
    alt B đang online
        RT-->>B: WebSocket message.created chứa envelope
    else B offline hoặc bỏ lỡ event
        Note over API,DB: Ciphertext đã nằm trong PostgreSQL
        B->>API: GET history khi mở thread, tải cũ hoặc lấy bù qua REST
        API->>DB: Đọc messages theo quyền và cursor
        DB-->>API: Messages chứa ciphertext
        API-->>B: Envelope và outer metadata
    end
    B->>B: Gộp UUID, kiểm tra context, sắp xếp seq
    alt Đã có cached message key
        B->>B: decryptWithKey kiểm tra content hash, AAD/tag và giải mã
    else Tin nhận lần đầu chưa cache
        B->>B: Lấy IK/SPK và private OPK đúng ID nếu header có OPK
        B->>B: open tính cùng SK, xác thực AAD/tag và giải mã AES-GCM
        opt Open thành công
            B->>B: Transaction IndexedDB cache key/context/hash/pin và xóa private OPK đã dùng
            B->>B: Chờ transaction.oncomplete
        end
    end
    alt Decrypt thành công và state local đã commit
        B->>B: Render plaintext theo seq
        opt Initial sync xong, owner, chat active, tab visible, cuối viewport và không loaded ciphertext lỗi/pending
            B->>API: PUT /threads/:id/read với last_read_seq hợp lệ
        end
    else Decrypt hoặc local commit lỗi
        B->>B: Hiện lỗi/placeholder, giữ state cũ và private OPK
        Note over B: Không render plaintext và không vượt read marker qua tin lỗi
    end
    Note over API,DB: Không nhận SK, private keys hoặc plaintext E2EE
```

`content_format` là `e2ee_v1`; `content` là **chuỗi JSON envelope** chứa `version`, `recipient_id`, `sender_identity_key`, `ephemeral_key`, `signed_prekey_id`, `one_time_prekey_id`, `nonce` và `ciphertext`. Outer message có UUID, thread/sender IDs, seq và thời điểm. Server thấy các public header và metadata này; lưu nguyên byte của content, không marshal lại.

API kiểm tra quyền và public header, không xác thực AEAD bằng cách giải mã; OPK public không còn trong DB sau claim vẫn hợp lệ cho message. Gateway XACK không chứng minh B đã nhận hoặc đọc. Cùng receive pipeline xử lý event và REST; cache key cho phép reload/duplicate đọc lại sau private OPK đã xóa, không consume lần hai.

Nếu header có OPK ID nhưng thiếu private OPK và cached key, client báo lỗi, **không chuyển sang 3DH**. AEAD hoặc local commit lỗi không làm mất private OPK. Read marker chỉ tăng sau decrypt, local commit, render và đủ điều kiện owner/viewport; nhận ciphertext hoặc lấy bù lịch sử chưa chứng minh đã đọc.

REST dùng trang/cursor `before_seq`/`next_cursor`; cache mới chụp `last_read_seq` từ summary rồi lấy trang mới nhất và đi lùi tới mốc đó hoặc hết lịch sử được API cho phép. Mốc 0 đi tới hết cursor; khoảng seq không được xem trong group không được cố lấp. Chỉ lượt thành công mới hoàn tất initial sync; page lỗi không cho read marker vượt phần chưa đồng bộ. Reconnect/gap tiếp tục lấy bù bằng REST, không polling; **Tin cũ hơn** tiếp tục từ trang cũ nhất đã lấy. Tin realtime mới có gap phải chờ lấy bù trước khi marker vượt `syncedSeq`. Evidence và giới hạn kiểm chứng của sửa đổi nằm trong hướng dẫn demo.

## 4. Vì sao A và B tính được cùng secret?

`priv` là private key, `pub` là public key. X25519 cho cùng DH output khi hai phía dùng cặp khóa đối ứng:

| Đầu ra DH | A tính bằng | B tính bằng |
| --- | --- | --- |
| DH1 | IK_A_priv và SPK_B_pub | SPK_B_priv và IK_A_pub |
| DH2 | EK_A_priv và IK_B_pub | IK_B_priv và EK_A_pub |
| DH3 | EK_A_priv và SPK_B_pub | SPK_B_priv và EK_A_pub |
| DH4, nếu có OPK | EK_A_priv và OPK_B_pub | OPK_B_priv và EK_A_pub |

Hai client nối cùng các DH outputs theo cùng thứ tự rồi chạy cấu hình đã chốt:

```text
KM = DH1 || DH2 || DH3 [|| DH4 nếu bundle có OPK]
SK = HKDF-SHA-256(
    input = 32 byte 0xFF || KM,
    salt  = 32 byte 0x00,
    info  = "Mini-Hermes/X3DH/v1",
    size  = 32 byte
)
```

A/B thu được cùng SK mà không truyền SK qua mạng. A dùng SK làm AES-256-GCM key, nonce ngẫu nhiên 12 byte và AAD; B dùng cùng key/nonce/AAD để xác thực và giải mã. Chi tiết DH và prekeys đối chiếu [X3DH §3.2–3.4](https://signal.org/docs/specifications/x3dh/#the-x3dh-protocol).

AAD ràng buộc hai IK public, `thread_id`, `message_id`, `sender_id`, `recipient_id`, EK public và SPK/OPK IDs theo byte layout trong contract; không ràng buộc seq hoặc thời điểm do server cấp. Giá trị khóa public/private trong JSON dùng Base64 chuẩn có padding của raw keys 32 byte; public key encoding khi ký/AAD là `0x05 || public32`.

Chữ ký SPK dùng **biến thể libsignal** `go.mau.fi/libsignal v0.2.2`; không cam kết canonical XEdDSA encoding hoặc tương thích đầy đủ Signal. Bridge Go/WASM chỉ xử lý crypto/encoding; không HTTP, JWT, IndexedDB hoặc DOM, và không có implementation crypto JavaScript riêng.

## 5. Hai chiều, retry và giới hạn cần duyệt

- **B→A:** đổi vai, claim bundle A, tạo EK/nonce/UUID/key mới. Không dùng lại secret nhận từ A→B để trả lời. Mỗi message mới là một lượt X3DH riêng, chưa có session Double Ratchet. [X3DH §4.3 — Replay and key reuse](https://signal.org/docs/specifications/x3dh/#replay-and-key-reuse).
- **Retry cùng tin:** giữ nguyên UUID/body/envelope/nonce đã lưu; không claim hoặc Seal lại. Transaction trả row/seq cũ, không insert/tăng seq; payload khác trả 409. Publish lại có thể tạo event trùng, client gộp UUID.
- **Đọc lại:** client giữ cached message keys trong IndexedDB cho own/peer history. Cache key và xóa private OPK của lần nhận mới là một transaction local. Lộ cả profile có thể lộ lịch sử đã cache; không mô tả đây là cơ chế xóa khóa của ratchet.
- **Identity:** chữ ký xác thực SPK với IK trong bundle. TOFU phát hiện đổi IK về sau nếu pin còn tin cậy; lần liên hệ đầu cần đối chiếu fingerprint/UUID qua kênh đã xác thực.
- **Nhiều tab:** gateway fan-out nhiều connections của recipient không đồng nghĩa nhiều tab cùng decrypt. Text hiện không echo sang connections khác của sender; đây chưa phải đồng bộ đầy đủ các tab cùng tài khoản.
- **Client web:** lấy IndexedDB, XSS hoặc thay mã JS/WASM có thể làm lộ keys/plaintext. WASM không cách ly khóa khỏi mã độc cùng trang. Mất state không tự khôi phục khóa hoặc thay IK/SPK đã đăng ký.

Các điểm cần người hướng dẫn xem xét: một client active/tài khoản, X3DH độc lập mỗi tin, fallback 3DH khi cạn OPK và phạm vi chưa có ratchet/rotation/backup. Giới hạn FS/PCS có điều kiện nằm trong ADR #4. Tài liệu này mô tả thiết kế và code đối chiếu, không phải báo cáo demo PASS hoặc xác nhận mentor đã duyệt.
