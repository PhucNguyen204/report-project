# Static analysis and typed FCM credentials implementation plan

> **For agentic workers:** dùng test-driven-development và verification-before-completion; root giữ Git. Agent review chờ quota như observability plan.

**Goal:** staticcheck thành gate bắt buộc, bỏ5findings hiện tại và đọc credential FCM theo loại được xác thực.

**Architecture:** Runtime config tiếp tục dùng ADC khi không chỉ định file. File FCM chỉ nhận service_account private0600,≤64KiB, Google token endpoint/default universe, email service account và RSA PKCS8≥2048bits hợp lệ. Chỉ sau kiểm tra mới gọi CredentialsFromJSONWithType; không fetch token lúc parse. Không chấp nhận credential type khác qua file này; federation/impersonation do ADC của môi trường triển khai quản lý.

**Tech Stack:** Go1.26.11, oauth2v0.38.0 typed loader, staticcheckv0.8.2 (module proxy13/11 xác nhận Go1.26 support).

**Spec:** F01 startup configuration/F37 quality gates trong plan09/11; warning SA1019 của SDK yêu cầu validate credential configuration trước khi dùng. Endpoint/type policy là lựa chọn có chủ đích cho FCM Google public cloud, không claim hỗ trợ custom universe/token proxy.

## Global constraints

- Không credential/key/JWT/token/email/path nhạy cảm trong lỗi hoặc log; key test được sinh trong tempdir, không commit.
- Không gọi Google/FCM thật trong unit; HTTP transport fixture phải được báo rõ.
- Không suppress staticcheck; sửa deprecated APIs và3error strings internal, giữ business codes/messages.
- Không sửa migration; mỗi checkpoint verify rồi commit/push/merge.

## Review focus

1. Type service_account nhưng token_uri độc hại phải bị chặn trước network.
2. File mở quyền/quá lớn/nonregular/badJSON/private key không hợp lệ phải fail startup an toàn.
3. Scope/audience của JWT ký thật phải đúng FCM/Google, không chỉ assert constructor nonnil.
4. Response replay vẫn giữ status/body/ETag sau bỏ HeaderMap deprecated; dùng integration có sẵn và trace replay mới.
5. Unit/integration/performance build tags phải cùng qua lint, không chỉ check default build.

### Task1: close findings and enforce lint

Files: internal/app/fcm.go +fcm_test.go, cmd/server/main.go; internal/platform/idempotency.go/queue.go, internal/apicontract/contract.go; Makefile/README; PROGRESS.md.

Interface: app.ReadFCMCredentials(ctx,path)(*google.Credentials,error). File được open/stat/read bounded, parse fields/type/private key/token_uri/universe/client_email rồi typed SDK loader với scope firebase.messaging. ADC branch giữ nguyên.

- [x] Baseline staticcheck RED5findings, thêm tests loader chưa có RED; invalid types/URLs/key/file bị từ chối trước HTTP, errors không echo.
- [x] Valid RSA fixture ký JWT thật; verify signature/iss/aud/scope qua RoundTripper kiểm soát, không gọi Google. Omitted token_uri dùng Google default.
- [x] Implement minimal loader, replace deprecated HeaderMap bằng Header(), lowercase internal errors; không đổi business behavior để qua lint.
- [x] Make lint pinnedv0.8.2 default +integration/performance tags, verify phụ thuộc lint. Full unit/integration/race/security và lint PASS, cập nhật report; Git ngay sau gate.

Review: tags mở rộng thêm6findings trong tests; bổ sung3status assertions, bỏ2assignments không dùng, t.Fatalf thay fmt.Sprintf. Test FIFO RED loader bị chặn ở open→O_NONBLOCK +stat regularfile→GREEN, không để goroutine treo. WeakRSA/nonRSA/invalidDER/private file/boundedJSON được kiểm. `make verify integration security`:520Go/0fail/0skip,7Python/global85.91%, app89.14/platform87.03; staticcheck cả2buildsets PASS, No vulnerabilities found. Unit transport fixture không thay Google/FCM live smoke.
