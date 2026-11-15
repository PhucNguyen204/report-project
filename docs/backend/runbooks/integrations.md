# Cấu hình mail, AI và thông báo đẩy

Backend dùng SMTP STARTTLS, Firebase Cloud Messaging HTTP v1 và OpenAI Responses. Không cần đổi Go, PostgreSQL, Redis, River hoặc S3. Model mặc định `gpt-5.6-terra`, có thể đổi bằng `OPENAI_MODEL`; cần kiểm quyền truy cập của project bằng key thực tế. [Tài liệu model](https://developers.openai.com/api/docs/models/gpt-5.6-terra) xác nhận Responses và Structured Outputs được hỗ trợ.

## Cấu hình cần cung cấp

Không dán key vào chat hoặc commit vào Git. Đặt file trong `backend/.local/`, quyền `0400` hoặc `0600`; `.local` và `.env` đã được ignore. Người vận hành tạo các credential trên tài khoản của mình.

| Dịch vụ | Cấu hình | Đích kiểm thử cần có |
|---|---|---|
| SMTP | `SMTP_ADDR=host:587`, `SMTP_FROM`, `SMTP_USER`, `SMTP_PASSWORD` trong `.env` local | Địa chỉ email staging do người dùng cung cấp |
| FCM | `FCM_PROJECT_ID`, `FCM_CREDENTIALS_FILE` trỏ file service account Firebase; hoặc ADC | FCM registration token của thiết bị staging thuộc project |
| OpenAI | `OPENAI_API_KEY_FILE` trỏ file chứa một API key, `OPENAI_MODEL` | Project có quyền gọi model và ngân sách API |

Native runtime dùng đường dẫn local, ví dụ `.local/openai.key`. Compose đã bind cả `.local` read-only vào `/run/secrets`; đặt `OPENAI_API_KEY_FILE=/run/secrets/openai.key` và `FCM_CREDENTIALS_FILE=/run/secrets/fcm.json` trong `.env` khi chạy container. Không đưa key vào `compose.yaml`, URL, log hoặc tham số dòng lệnh.

Production yêu cầu SMTP STARTTLS, FCM và HTTPS S3; SMTP cổng465 dùng TLS ngay từ đầu không phải giao thức của adapter hiện tại. Mailpit chỉ dùng ở local, không chuyển tiếp thư ra internet. Đăng ký/xác thực và đặt lại mật khẩu tạo email trong encrypted River queue; thông báo phân công, sắp hết hạn và hoàn tất AI đi qua inbox/FCM và thiết lập nhắc của người nhận.

## Luồng AI hỗ trợ UC18

`POST /it4788/v1/spaces/{sid}/ai-recommendations` nhận `date`, `mealSlot`, `servings`, `limit` (tối đa10), yêu cầu JWT, `X-App-Version`, `Idempotency-Key`; trả `202` và job. Dùng `GET /it4788/v1/spaces/{sid}/ai-recommendations/{jobId}` để đọc trạng thái `pending`, `ready`, `failed`. Chỉ chủ job hiện còn quyền trong nhóm được đọc. Kết quả hết hạn sau24giờ.

Backend chọn tối đa10 công thức đang hiển thị rồi gửi tên, tỉ lệ đáp ứng, tên nguyên liệu thiếu/sắp hết hạn và alias `r01`…`r10`. Không gửi UUID tài khoản/nhóm/công thức, email, token hoặc nội dung chat. AI chỉ xếp hạng và giải thích; backend kiểm alias, số kết quả và quyền công thức. Lượng, tồn kho, lịch bữa ăn và hành động đã nấu vẫn do nghiệp vụ hiện có quyết định.

Responses dùng `store=false`, Structured Outputs, không tools hoặc lịch sử hội thoại. `store=false` không phải cam kết zero retention của tài khoản; xem [quy định dữ liệu API](https://developers.openai.com/api/docs/guides/your-data). SDK không tự retry; job tối đa2 lần thử provider, giới hạn10 job được chấp nhận/người/ngày UTC. Replay cùng idempotency key không tạo job/quota mới. Hết quota nhận429; chưa cấu hình key nhận503 sau khi kiểm quyền. Endpoint gợi ý deterministic hiện có tiếp tục hoạt động.

## Bằng chứng và giới hạn

Chạy smoke có chủ đích từ `backend/`, sau khi nạp cấu hình local vào environment:

```sh
go run ./cmd/integration-smoke
go run ./cmd/integration-smoke --mail --push --ai
```

Lệnh đầu không gọi provider, trả JSON `NOT_RUN/not_requested`. Lệnh thứ hai chỉ gọi dịch vụ có đủ cấu hình: thêm `SMOKE_EMAIL_TO` cho SMTP, `SMOKE_FCM_TOKEN` cho FCM. Thiếu biến trả `NOT_RUN/missing_configuration` kèm tên biến thiếu; không in giá trị. Provider bị lỗi trả `FAIL`, mã thoát1 và thông báo đã chuẩn hóa. JSON `PASS` chỉ xác nhận provider chấp nhận lời gọi; cần kiểm inbox email/thiết bị để xác nhận thư hoặc push đã đến người nhận. Không đưa secret trực tiếp vào tham số shell.

Unit test dùng HTTP server có kiểm soát; integration dùng PostgreSQL/Redis/River, S3 và SMTP Mailpit thật ở local. Các phép thử này không chứng minh OpenAI/FCM hoặc email internet đã hoạt động. Smoke staging phải có credential và đích kiểm thử trong bảng trên; thiếu cấu hình ghi `NOT_RUN`, không ghi `PASS`. Không dùng email/token tổng hợp của fixture để gửi qua internet.

Kết quả thực thi, checklist và commit được cập nhật trong [báo cáo chung](../PROGRESS.md). CI GitLab cần runner online đủ điều kiện; credential provider không khắc phục job chưa được runner nhận.
