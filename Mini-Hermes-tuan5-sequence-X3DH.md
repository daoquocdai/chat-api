# Mini-Hermes — Tuần 5: sequence diagram X3DH

**Trạng thái:** thiết kế đề xuất để người hướng dẫn xem xét.

Mục tiêu là gửi tin direct E2EE hai chiều trên web: đăng ký public bundle → lấy bundle của người nhận → tính shared secret tại client → mã hóa và gửi ciphertext. Private keys và shared secret nằm ở client; server lưu public keys, ciphertext và metadata.

## 1. Phạm vi và ký hiệu

Mỗi tài khoản demo dùng một browser profile cố định, một tab E2EE hoạt động. Crypto chạy trong Go/WASM; JavaScript quản lý REST, WebSocket và IndexedDB. Nhóm và direct plaintext dùng luồng hiện có. Chưa triển khai Double Ratchet, E2EE nhóm, multi-device hoặc backup/rotation khóa.

| Ký hiệu | Ý nghĩa | Vị trí lưu |
| --- | --- | --- |
| IK | Identity key — cặp khóa định danh của tài khoản | Private ở client; public ở server |
| SPK | Signed prekey — cặp khóa được ký bằng IK private | Private ở client; public và chữ ký ở server |
| OPK | One-time prekey — cặp khóa dùng cho một lượt X3DH | Private ở client; public được server cấp một lần |
| EK | Ephemeral key — cặp khóa mới của bên gửi cho mỗi lượt | Private dùng tại client gửi; public đi trong envelope |
| SK | Shared secret — khóa 32 byte dẫn xuất từ DH/HKDF | Tính độc lập tại hai client; không gửi lên server |
| AAD | Associated data — ngữ cảnh được xác thực cùng ciphertext | Tạo từ public IK và context/header của tin |

**Public bundle** gồm IK public, SPK public kèm ID/chữ ký và các OPK public. Upload chứa một batch OPK; claim trả tối đa một OPK. Bản demo khởi tạo 20 OPK, giữ IK/SPK trong vòng đời demo.

## 2. Sơ đồ đăng ký và lấy bundle

A và B đã đăng nhập. Sau khi cả hai đăng ký public bundle, A mở direct thread có chế độ `e2ee`. Thread plaintext đã tồn tại không được tự chuyển sang E2EE.

```mermaid
sequenceDiagram
    participant A as Client A
    participant API as chat-api
    participant DB as PostgreSQL
    participant B as Client B

    par A đăng ký
        A->>A: Sinh IK, SPK, 20 OPK; ký SPK
        A->>A: Lưu private keys vào IndexedDB
        A->>API: POST /e2ee/prekeys — public bundle
        API->>DB: Transaction lưu public keys A
        API-->>A: Xác nhận sau commit
    and B đăng ký
        B->>B: Sinh IK, SPK, 20 OPK; ký SPK
        B->>B: Lưu private keys vào IndexedDB
        B->>API: POST /e2ee/prekeys — public bundle
        API->>DB: Transaction lưu public keys B
        API-->>B: Xác nhận sau commit
    end
    Note over B: B có thể offline sau đăng ký
    A->>API: POST /threads/direct — mode e2ee
    API->>DB: Kiểm tra bundle; tạo hoặc mở direct thread
    API-->>A: thread_id
    A->>A: Tạo UUID cho lần gửi mới
    A->>API: POST /e2ee/bundles/:B/claim — thread_id
    API->>DB: Transaction kiểm tra quyền; khóa user B
    API->>DB: Đọc IK/SPK; DELETE RETURNING OPK nhỏ nhất
    DB-->>API: Public bundle với một OPK hoặc null
    API->>DB: COMMIT
    API-->>A: Trả bundle sau commit
```

Các API nghiệp vụ dùng JWT. `:B` là UUID tài khoản B. Claim chỉ hợp lệ khi A và B là hai participant active của direct E2EE thread.

Public OPK được xóa trong transaction claim để không cấp lại cho lượt khác. Private OPK tương ứng vẫn ở B, chờ giải mã tin sử dụng nó. Nếu response claim bị mất, OPK đã cấp có thể bị hao; server không hoàn trả OPK. Khi hết OPK, bundle trả `null` và lượt mới dùng 3DH.

## 3. Sơ đồ tính secret, gửi và giải mã ciphertext

Đầu vào của A là bundle từ sơ đồ 2. Trong sơ đồ dưới, **Phát realtime** gộp Redis Stream và ws-gateway thành một cột để tập trung vào luồng E2EE; đây vẫn là hai thành phần riêng: API dùng XADD, gateway đọc Stream rồi phát WebSocket.

```mermaid
sequenceDiagram
    participant A as Client A
    participant API as chat-api
    participant DB as PostgreSQL
    participant RT as Phát realtime
    participant B as Client B

    A->>A: Kiểm tra chữ ký SPK và identity pin
    Note over A: Chữ ký sai hoặc pin đã biết bị đổi: dừng
    A->>A: Sinh EK mới; tính DH và HKDF ra SK
    A->>A: Tạo AAD và nonce; AES-GCM mã hóa plaintext
    A->>A: Lưu message key và nguyên pending body vào IndexedDB
    A->>API: POST /threads/:id/messages — UUID và envelope
    API->>DB: Transaction kiểm tra quyền/mode/header/retry
    API->>DB: Cấp seq và lưu ciphertext; COMMIT
    DB-->>API: Message đã lưu
    API->>RT: XADD message.created chứa ciphertext
    API-->>A: Response message sau publish thành công
    A->>A: Xác nhận response; commit bỏ pending
    alt B đang online
        RT-->>B: WebSocket event chứa envelope
    else B offline hoặc bỏ lỡ event
        Note over API,DB: Ciphertext đã có trong PostgreSQL
        B->>API: Khi quay lại, GET history theo cursor
        API->>DB: Đọc messages trong phạm vi được phép
        DB-->>API: Messages chứa ciphertext
        API-->>B: Envelope và metadata
    end
    B->>B: Gộp UUID; lấy private keys theo prekey IDs
    B->>B: Tính cùng SK; xác thực AAD/tag và giải mã AES-GCM
    B->>B: Commit cached key/pin và xóa private OPK đã dùng
    B->>B: Render plaintext theo seq
    Note over B: Lỗi decrypt hoặc local commit: giữ state cũ
    Note over API,DB: Không nhận SK, private keys hoặc plaintext E2EE
```

`content_format` là `e2ee_v1`; `content` là chuỗi JSON envelope chứa recipient ID, IK public của sender, EK public, SPK/OPK IDs, nonce và ciphertext. Outer message có UUID, thread/sender IDs, seq và thời điểm. Các trường này không được che khỏi server.

API kiểm tra quyền và public header; không kiểm tra AEAD bằng cách giải mã. Gateway ACK công việc không có nghĩa B đã nhận hay đọc tin. Read marker chỉ tăng khi web thỏa điều kiện decrypt, local commit, render và đang xem chat.

## 4. Vì sao hai client tính được cùng secret?

`priv` là private key, `pub` là public key. Hai phía dùng các cặp đối ứng của X25519:

| Đầu ra DH | A tính bằng | B tính bằng |
| --- | --- | --- |
| DH1 | IK_A_priv và SPK_B_pub | SPK_B_priv và IK_A_pub |
| DH2 | EK_A_priv và IK_B_pub | IK_B_priv và EK_A_pub |
| DH3 | EK_A_priv và SPK_B_pub | SPK_B_priv và EK_A_pub |
| DH4, nếu có OPK | EK_A_priv và OPK_B_pub | OPK_B_priv và EK_A_pub |

Theo cấu hình demo:

```text
KM = DH1 || DH2 || DH3 [|| DH4 nếu bundle có OPK]
SK = HKDF-SHA-256(
    input = 32 byte 0xFF || KM,
    salt  = 32 byte 0x00,
    info  = "Mini-Hermes/X3DH/v1",
    size  = 32 byte
)
```

Hai client thu được cùng SK mà không truyền SK qua mạng. A dùng SK làm khóa AES-256-GCM, nonce ngẫu nhiên 12 byte và AAD. B dùng cùng SK, nonce và AAD để xác thực/giải mã. Chi tiết DH và prekey đối chiếu [X3DH §3.2–3.4](https://signal.org/docs/specifications/x3dh/#the-x3dh-protocol).

AAD của demo ràng buộc hai IK public, thread/message/sender/recipient UUID và prekey header; không ràng buộc seq hoặc thời điểm do server cấp. Chữ ký SPK dùng biến thể của `go.mau.fi/libsignal v0.2.2`; không cam kết canonical XEdDSA encoding hoặc tương thích đầy đủ với Signal.

## 5. Hai chiều, retry và giới hạn cần duyệt

- **B→A:** đổi vai A/B, claim bundle A và tạo EK/nonce/UUID mới. Không dùng lại secret nhận từ A→B để trả lời. Mỗi message mới là một lượt X3DH riêng, chưa có session Double Ratchet.
- **Retry cùng tin:** dùng lại nguyên UUID/body/envelope/nonce đã lưu; không claim hoặc mã hóa lại. Repository trả row và seq cũ. Nếu DB commit nhưng publish lỗi, response có thể là 503; retry có thể publish lại event, client gộp theo UUID.
- **Đọc lại:** client giữ cached message keys trong IndexedDB để decrypt lịch sử sau reload. B chỉ bỏ private OPK sau decrypt và local commit thành công. Giữ key lịch sử là một đánh đổi bảo mật, không phải cơ chế xóa khóa của ratchet.
- **Identity:** chữ ký xác thực SPK với IK trong bundle. TOFU phát hiện thay đổi identity về sau; xác nhận người thật vẫn cần đối chiếu fingerprint qua kênh tin cậy.
- **Nhiều tab:** một tab sở hữu state E2EE; tab thứ hai chờ tiếp quản. Fan-out nhiều WebSocket connection không đồng nghĩa nhiều tab cùng decrypt được.

Các điểm cần người hướng dẫn xem xét: chấp nhận demo web với một client active/tài khoản; X3DH độc lập mỗi message; fallback 3DH khi cạn OPK; và giới hạn chưa có Double Ratchet/rotation/backup. Tài liệu này mô tả thiết kế, không phải báo cáo PASS hoặc xác nhận đã được duyệt.
