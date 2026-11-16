# Backend Đi chợ tiện lợi

Go modular monolith với PostgreSQL, River worker, Redis GCRA, SMTP, FCM và S3 riêng tư. Theo dõi phạm vi25 UC, kết quả kiểm thử và các mục còn mở trong [báo cáo chung](../docs/backend/PROGRESS.md) và [plan](../docs/superpowers/plans/2026-11-09-backend.md).

## Chạy development

Yêu cầu Go≥1.26.11, Python≥3.9, Docker và Docker Compose. Kiểm thử PDF cần Poppler (`pdftotext`): `brew install poppler` trên macOS hoặc gói `poppler-utils` trên Linux.

```sh
cd backend
make init
make deps
make run
```

`init` tạo khóa riêng0600 và `.env` từ `.env.example`, giữ nguyên file đã có. Cấu hình được truyền qua environment, không thực thi nội dung `.env` bằng shell. Các mật khẩu trong example/Compose chỉ dành cho fixture development.

API tại `http://127.0.0.1:58080`; Mailpit tại `http://127.0.0.1:58025`. PostgreSQL55432, Redis56379, S359000, SMTP51025. DB khởi tạo có danh mục/đơn vị/thực phẩm mẫu; đăng ký rồi lấy OTP từ Mailpit. Không có tài khoản admin với mật khẩu mặc định.

Chạy cả API và worker trong container:

```sh
make up
make smoke
make down
```

`down` giữ volume. Không xóa volume chứa dữ liệu cần giữ. Khi cổng đã được dùng, cấu hình `POSTGRES_PORT`, `REDIS_PORT`, `S3_PORT`, `SMTP_PORT`, `MAILPIT_PORT`, `API_PORT` và dùng tên project Compose riêng. Các container hạ tầng có healthcheck; app chờ dependency sẵn sàng. UID/GID local trong `.env` giúp container nonroot đọc khóa bind read-only.

S3 development dùng SeaweedFS tương thích S3, đã kiểm thử bằng AWS SDK và URL ký thật. Đây không phải bằng chứng MinIO/AWS production đã được smoke.

## API và cấu hình

Hợp đồng tại `/openapi.json`, nguồn [api/build.py](api/build.py) và artifact [api/openapi.json](api/openapi.json). `make openapi` sinh lại và kiểm không có diff. Có101 operations,86 mã API trong phạm vi; Y01/Y02 hoãn, không có mã vạch/P08.

- Base path `/it4788/v1`; JSON và `X-App-Version: 1.0.0`.
- Authenticated commands dùng `Idempotency-Key` dạng UUID. Public auth và preview dryRun có ngoại lệ trong hợp đồng; logout vẫn cần khóa.
- Mutation có version dùng `If-Match` dương, nhận cả dạng `1` và `"1"`. Metadata PATCH có merge từng trường; thay đổi số lượng/quyền dùng chính sách xung đột ghi rõ trong contract.
- Lượng gửi bằng chuỗi hoặc số có tối đa2 chữ số thập phân, tiền VND nguyên; ngày nghiệp vụ theo Asia/Ho_Chi_Minh.

`APP_ENV` là development hoặc production; `MODE` là api, worker hoặc all. Server đọc khóa Ed25519 và queue AES từ file0600 riêng. Queue key phải giữ được qua xoay signing key để đọc job tồn đọng. Production cần HTTPS S3, SMTP STARTTLS và FCM credentials/ADC hợp lệ.

`FCM_CREDENTIALS_FILE` chỉ nhận file service_account private (0400/0600), tối đa64KiB, RSA PKCS8≥2048bits, Google public-cloud universe/token endpoint. Không dùng custom token proxy qua file này. Bỏ trống để dùng ADC của môi trường triển khai. Loader không gọi Google lúc đọc file; kiểm chứng FCM thật vẫn cần credential và thiết bị staging.

AI hỗ trợ UC18 qua job chạy nền, dùng OpenAI Responses và key từ `OPENAI_API_KEY_FILE` (file0400/0600, tối đa4KiB); không có key thì trả503. `OPENAI_MODEL` mặc định `gpt-5.6-terra`. Xem [cấu hình mail/AI/push](../docs/backend/runbooks/integrations.md) về endpoints, quota, dữ liệu gửi provider và các credential/đích staging cần cung cấp.

M06 hỗ trợ nguyên liệu lấy ngoài tủ (lượng0), chọn nhiều lô, actor/thời điểm đã nấu và replay không trừ kho lần nữa. Xem [hợp đồng xác nhận nấu](../docs/backend/runbooks/cooking.md) về lượng cơ sở trong deductions, lỗi409/current và tương thích client cũ.

`S3_ENDPOINT` dùng cho I/O nội bộ. `S3_PUBLIC_ENDPOINT` dùng khi ký URL cho client, mặc định bằng endpoint nội bộ. Cả hai dùng cùng bucket/key/region/credentials; không sửa host của URL sau khi ký. Compose đặt internal=`http://s3:8333`, public=`http://127.0.0.1:59000`.

Ảnh được kiểm quyền theo vai trò hiện tại trong DB. Admin quản lý ảnh template và ảnh đại diện của mình; membership cũ không cho admin truy cập ảnh gia đình/ảnh thực phẩm riêng.

`TRUSTED_PROXIES` là danh sách CIDR của proxy thực sự tin cậy. Chuỗi X-Forwarded-For được đọc từ phía proxy về client đầu tiên không tin cậy. Bỏ trống để dùng địa chỉ kết nối trực tiếp.

Runtime có cổng vận hành riêng `OPS_LISTEN_ADDR` (mặc định127.0.0.1:8081), kể cả worker-only. Compose không publish cổng này; Prometheus scrape `/metrics` qua mạng nội bộ. Log JSON và lỗi API có mã trace; đặt `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` URL đầy đủ khi có collector, bỏ trống để không export. Xem [runbook observability](../docs/backend/runbooks/observability.md) về labels, sampling, queue propagation và giới hạn đo SLA.

## Kiểm chứng

```sh
make verify
make lint
make integration
make security secrets
make fuzz
make smoke
```

`verify` chạy format/staticcheck/vet/mod verify/unit Python/Go race/build. `integration` cần PostgreSQL16, Redis7, S3 và Mailpit thật; tạo DB riêng từng test, không skip khi dịch vụ thiếu. Artifact tại `../output/backend-qa/{integration.jsonl,coverage.out,coverage-summary.json}`. Coverage dùng `-coverpkg=./...`, gate total85%, domain/app/platform85%, kernel/core90%; test fail/skip làm gate fail. `smoke` cần API/worker đang chạy, thực hiện K1–K5 với tài khoản tổng hợp, OTP SMTP, ảnh S3 và CSV/PDF; không in token/OTP/mật khẩu.

`fuzz` chạy 5 campaign thực tế, mỗi campaign 15 giây/2 workers; log `../output/backend-qa/fuzz-*.log`. Quantity/JSON/FEFO/Missing/civil dates có invariants riêng. Seed tests trong `verify` không thay campaign; lỗi từ `go test` qua `tee` vẫn làm make fail, kể cả Make 3.81 trên macOS.

Đổi địa chỉ fixture cho CI hoặc project riêng:

```sh
TEST_DATABASE_URL='postgres://dicho:dicho-local-test@127.0.0.1:55433/dicho_test?sslmode=disable' \
TEST_REDIS_URL='redis://127.0.0.1:56380/0' \
TEST_S3_ENDPOINT='http://127.0.0.1:59001' \
TEST_SMTP_ADDR='127.0.0.1:51026' \
TEST_MAILPIT_URL='http://127.0.0.1:58026' make integration
```

`.gitlab-ci.yml` chạy verify/security và integration với dịch vụ thật, lưu test/coverage14 ngày. Trạng thái CI remote chỉ được ghi PASS sau khi có kết quả runner; cấu hình CI không tự deploy.

Khôi phục fixture QA PostgreSQL đang chạy, giữ source hoạt động và restore vào database sinh mới:

```sh
make recovery
```

Cần stack `dicho-qa` đã có dữ liệu K1–K5. Lệnh kiểm snapshot/count/SHA256/tiền/lượng và retry River, dọn đúng database tạm, giữ archive0600 trong output. Xem [runbook recovery](../docs/backend/runbooks/recovery.md) để đổi nguồn/đọc giới hạn; đây là phép kiểm DB local, chưa gồm external S3/PITR hay RPO/RTO production.

Production tách migrator khỏi role API/worker: chạy `dicho-migrate` với DSN schema owner trước, rồi dùng role DML và `AUTO_MIGRATE=false`. Xem [runbook database roles](../docs/backend/runbooks/database-roles.md). Runtime kiểm checksum/schema thay vì tự sửa DDL; development mặc định auto migrate.

Đo tải local bằng `make performance`: fixture riêng16 user, mỗi scope500 recipes/200 lots, 50RPS×30s qua TCP/auth/Redis thật. Xem [runbook performance](../docs/backend/runbooks/performance.md); ngưỡng và lỗi được assert, artifact không chứa token/secret.

## Trạng thái release

Local workflow và unit/integration/race đã có bằng chứng trong báo cáo. Còn mở: keyset/default pagination ở một số endpoint, mở rộng tải ghi/Q01, backup production/PITR, monitoring, CI remote và smoke FCM production. Phép thử role DB giới hạn chạy trên fixture riêng; quyền database production chưa được thay đổi. Chưa xác nhận SLA/RPO/RTO từ một lần smoke local.
