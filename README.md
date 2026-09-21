# Chat API

Chat API là HTTP server viết bằng Go, dùng Gin để định tuyến và PostgreSQL để lưu dữ liệu của module user. Docker Compose khởi tạo cả PostgreSQL và Redis; ở phiên bản hiện tại, ứng dụng chưa kết nối Redis và chưa phát sự kiện qua Redis.

## Trạng thái hiện tại

API đang có:

| Method | Path | Chức năng |
| --- | --- | --- |
| `GET` | `/health` | Kiểm tra server đang hoạt động |
| `POST` | `/users` | Tạo người dùng |
| `GET` | `/users/:id` | Lấy người dùng theo `external_id` |

Trường `id` trong JSON response là `external_id` dạng UUID dùng bên ngoài API, không phải cột số tự tăng `users.id` trong PostgreSQL. Dự án chưa có đăng nhập/JWT và chưa có giao tiếp realtime (WebSocket hoặc cơ chế tương tự).

Luồng chi tiết của `POST /users` được ghi tại [docs/request-flow.md](docs/request-flow.md).

## Chạy từ bản clone mới

### Công cụ cần có

- Go theo phiên bản ghi trong `go.mod`.
- Docker Desktop hoặc Docker Engine có Docker Compose.
- [Goose](https://github.com/pressly/goose) để chạy migration.
- GNU Make nếu muốn dùng các tác vụ trong `Makefile`; Windows có thể dùng trực tiếp các lệnh ở phần dưới.
- sqlc chỉ cần thiết khi sửa file query và cần sinh lại code, không cần để chạy bản code đã clone.

Cài Goose và sqlc khi cần:

```bash
go install github.com/pressly/goose/v3/cmd/goose@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
```

Bảo đảm thư mục chứa Go binary (thường là `GOPATH/bin`) nằm trong `PATH`.

### Chuẩn bị và chạy

Sao chép config mẫu. Trên Windows CMD:

```bat
copy config\config.yml.example config\config.yml
```

Trên macOS/Linux:

```bash
cp config/config.yml.example config/config.yml
```

Không thêm `config/config.yml` vào Git vì đây là config cục bộ và có thể chứa thông tin kết nối nhạy cảm; file này đã được khai báo trong `.gitignore`.

Cài các Go module, khởi động dịch vụ Docker, chạy migration rồi chạy server:

```bash
go mod download
make up
make migrate
make run
```

Nếu Windows chưa có `make`, chạy các lệnh tương ứng trong CMD:

```bat
go mod download
docker compose up -d
set "DATABASE_URL=postgres://chat:chat@localhost:5432/chat_api?sslmode=disable"
goose -dir db/migrations postgres "%DATABASE_URL%" up
go run ./cmd
```

Server mặc định chạy tại `http://localhost:8080`.

Ứng dụng đọc `database_url` từ `config/config.yml`. Tác vụ `make migrate` của Goose lại đọc biến môi trường `DATABASE_URL` (hoặc dùng giá trị mặc định trong `Makefile`). Hai giá trị này phải trỏ tới cùng một database.

Các tác vụ Makefile hiện có và lệnh trực tiếp tương ứng:

| Tác vụ | Lệnh trực tiếp |
| --- | --- |
| `make run` | `go run ./cmd` |
| `make test` | `go test ./...` |
| `make build` | `go build ./...` |
| `make up` | `docker compose up -d` |
| `make migrate` | `goose -dir db/migrations postgres "%DATABASE_URL%" up` |
| `make migrate-status` | `goose -dir db/migrations postgres "%DATABASE_URL%" status` |
| `make sqlc` | `sqlc generate` |
| `make migrate-create NAME=ten_migration` | `goose -dir db/migrations create ten_migration sql` |

## Thử API trên Windows CMD

Kiểm tra server:

```bat
curl.exe http://localhost:8080/health
```

Tạo người dùng (đổi username nếu tên đã tồn tại):

```bat
curl.exe -X POST http://localhost:8080/users -H "Content-Type: application/json" -d "{\"username\":\"alice_readme\"}"
```

Sao chép giá trị `id` trong response để lấy lại người dùng:

```bat
curl.exe http://localhost:8080/users/THAY_UUID_VAO_DAY
```

## Kiểm tra code

Chạy test:

```bash
make test
```

Hoặc chạy trực tiếp trên mọi hệ điều hành:

```bash
go test ./...
go vet ./...
go build ./...
```

Các unit test của module user dùng đối tượng giả (fake) cho service hoặc repository. Chúng kiểm tra logic từng lớp nhanh và không cần PostgreSQL, nhưng không chứng minh migration, câu SQL, kết nối database hay toàn bộ luồng HTTP thật đang hoạt động cùng nhau. Muốn kiểm tra các phần đó cần chạy PostgreSQL, migration, server và gọi API thực tế.
