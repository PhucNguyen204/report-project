# Đo tải API trong môi trường local

Harness tại `backend/internal/app/performance_integration_test.go` chạy riêng với build tag `performance`. Nó dùng `NewSystem`, router và xác thực EdDSA thật qua HTTP TCP loopback; không gọi trực tiếp handler và không thay middleware xác thực hoặc giới hạn tần suất.

Từ thư mục `backend/`, khi PostgreSQL, Redis và S3 trong compose đang sẵn sàng:

```sh
go test -tags performance ./internal/app -run '^TestPerformance' -count=1 -v -timeout=10m
```

Các fixture mặc định dùng PostgreSQL `127.0.0.1:55432`, Redis `127.0.0.1:56379/0` và S3 `127.0.0.1:59000`. Có thể thay địa chỉ bằng `TEST_DATABASE_URL`, `TEST_REDIS_URL`, `TEST_S3_ENDPOINT`; không ghi URL chứa mật khẩu vào báo cáo hoặc nhật ký. Tài khoản PostgreSQL của fixture cần quyền tạo/xóa cơ sở dữ liệu. S3 dùng thông tin truy cập local giống các bài integration hiện có.

Trước khi chạy, thống nhất một khoảng yên tĩnh với các tác vụ khác dùng cùng máy hoặc cùng PostgreSQL. Harness không tự dừng dịch vụ hay tiến trình của người dùng.

## Dữ liệu và quyền truy cập

Mỗi lần chạy tạo một database mới, áp dụng migrations hiện hành, rồi seed 16 người dùng với 16 không gian cá nhân độc lập. Mỗi không gian có đúng 500 công thức, 5 nguyên liệu/công thức (1 nguyên liệu tùy chọn), 200 lô đang hoạt động và 20 công thức yêu thích. Toàn database có thêm 250 thực phẩm dùng chung. Số lượng của từng không gian được đọc lại và kiểm tra trước khi đo; bước `ANALYZE` hoàn tất trước warmup.

Người dùng và các họ phiên được seed bằng SQL. JWT được ký bằng khóa EdDSA mới sinh, có phiên chưa thu hồi trong PostgreSQL, và được kiểm tra qua middleware thật. Một JWT bị sửa chữ ký phải nhận 401 trước khi bắt đầu đo. Không ghi JWT, khóa ký, khóa mã hóa hàng đợi hoặc thông tin kết nối có mật khẩu vào artifact.

Redis dùng các khóa quota thực tế được băm từ UUID người dùng mới sinh. Harness kiểm tra 32 khóa dự kiến chưa tồn tại trước khi chạy và chỉ xóa chính những khóa đó khi kết thúc; không dùng `FLUSHDB`. Bucket S3 riêng được kiểm tra bằng PUT và GET qua URL ký thật, rồi được xóa khi cleanup. Bốn route đo tải không truy cập object storage, nên kết quả không phải benchmark S3. River worker được khởi động; scheduler không chạy trong kịch bản chỉ đọc này.

## Cách tạo tải và đánh giá

Sau 5 request warmup cho mỗi người dùng, harness phát đúng 1.500 request trong cửa sổ 30 giây, với lịch phát mở 50 request/giây. Bốn route có cùng 375 request:

| Route | Ngưỡng p95 local |
|---|---:|
| `GET /spaces/{sid}/lots` | 1.000 ms |
| `GET /spaces/{sid}/recipes?source=space` | 1.000 ms |
| `GET /spaces/{sid}/recipe-suggestions?servings=2&limit=10` | 2.000 ms |
| `GET /spaces/{sid}/foods/suggest?q=Performance+food&limit=10` | 500 ms |

Mỗi người dùng nhận đủ cả bốn loại request; người nhận nhiều nhất có 101 request tính cả warmup, thấp hơn quota 120 request/phút. Autocomplete vẫn đi qua quota 20 request/giây/người. Không tăng quota hoặc thay Redis bằng bộ đếm giả để làm bài đo vượt qua.

Có 128 worker HTTP và hàng chờ tối đa 128 request. Nếu hàng chờ đầy, request được ghi thành lỗi tạo tải thay vì bị bỏ qua. HTTP timeout là 10 giây. Độ trễ dùng để so ngưỡng được tính từ **thời điểm dự kiến phát** đến khi đọc xong body, vì vậy gồm thời gian chờ hàng, trễ lịch phát và chờ pool/kết nối. Artifact cũng ghi riêng thời gian từ lúc dispatch và độ trễ dispatch. Percentile dùng nearest rank.

Mọi lỗi HTTP, 429, timeout, body không đúng dữ liệu fixture và request bị loại do hết dung lượng đều được đếm. Test thất bại nếu có lỗi, thiếu mẫu, vượt p95 hoặc quan sát thấy Redis fallback. Thông lượng thành công được tính trên thời gian thực gồm cả phần chờ request cuối hoàn tất; không chỉ lấy cấu hình 50 RPS làm kết quả thông lượng.

## Artifact và giới hạn kết luận

Kết quả được ghi vào [`output/backend-qa/performance.json`](../../../output/backend-qa/performance.json), nằm trong thư mục đã được Git ignore. Artifact gồm thông tin CPU/RAM/Docker/Go, HEAD và danh sách file đang thay đổi tại thời điểm bắt đầu, dữ liệu, giới hạn, số mẫu, lỗi, thông lượng, p50/p95/max từng route và trạng thái cleanup. Artifact được ghi cả khi test thất bại; kiểm tra `status` và `phase` trước khi sử dụng số liệu.

Đây là phép đo trên binary được build từ working tree tại thời điểm chạy, không mặc nhiên đại diện cho toàn bộ verified HEAD. Kết quả local qua loopback không chứng minh độ trễ trên 4G, giao diện thiết bị, TLS, tải ghi, FCM/SMTP, khả năng phục hồi hay SLA production. Các tác vụ cùng dùng CPU, RAM, Docker hoặc PostgreSQL có thể ảnh hưởng kết quả; khi so sánh hai lần chạy phải giữ môi trường và ghi nhận tải nền tương đương.
