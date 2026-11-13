# Backend runtime packaging implementation plan

> Người dùng đã chọn triển khai tự động và root giữ Git. Dùng TDD và verification-before-completion; tiếp nối F01/F09/F37 trong plan09/11.

**Goal:** fresh checkout chạy được API/worker cùng PostgreSQL, Redis, S3 và SMTP; signed URL vẫn truy cập được từ máy client.

**Architecture:** Go binary chung cho api/worker/all; Compose cung cấp môi trường development. S3 dùng endpoint nội bộ cho I/O và endpoint công khai cho presigning, cùng bucket/key/credentials. Production dùng secrets ngoài image và runner kiểm thử riêng.

**Tech Stack:** Go1.26.11, Docker Compose, PostgreSQL16, Redis7, SeaweedFS S3, Mailpit; dependency và image khóa version/digest.

**Spec:** `docs/superpowers/plans/2026-11-09-backend.md`, `chapters/c3/bao-mat.tex`, `chapters/c3/csdl.tex`.

## Constraints and review focus

- Không nhúng private key hoặc credential production trong Git/image/log; key0600, runtime nonroot.
- Không đổi migration đã commit. Port fixture mặc định giữ55432/56379/59000/51025/58025.
- Endpoint S3 công khai phải nằm trong chữ ký gốc; không thay host bằng string sau khi ký.
- Không tự dừng/xóa volume fixture đang có; kiểm chứng Compose bằng project và port riêng.
- Host và container đều nhận URL client dùng được; dependency unhealthy không bị báo là app đã sẵn sàng.

## Task 1: Public S3 presigner

Files: `internal/modules/media/s3.go`, `s3_test.go`, `internal/app/config.go`, `config_test.go`, `cmd/server/main.go`.

- [ ] Test `TestPublicS3ClientSignsPublicURLsWhileWritesUseInternalEndpoint`: upload/download URL mang host public, Put chỉ tới internal, base store không bị đổi cấu hình. Quan sát RED vì thiếu capability.
- [ ] `(*S3Store).WithPublicClient(*s3.Client) *S3Store` trả store mới với presigner riêng; NewS3 giữ hành vi cũ.
- [ ] `RuntimeConfig.S3PublicEndpoint` đọc S3_PUBLIC_ENDPOINT, mặc định S3_ENDPOINT; production cả hai HTTPS.
- [ ] Main tạo public client cùng region/credentials và nối cho media/exports.
- [ ] Unit/integration/race/full regression, review, báo cáo, commit/push với Feature-ID F09-public-presigner.

## Task 2: Reproducible development and CI

Files: `backend/{Dockerfile,.dockerignore,compose.yaml,.env.example,Makefile,README.md}`, `backend/scripts/dev.py`, `.gitlab-ci.yml`.

- [ ] Compose dependency có healthcheck; API profile app, secrets bind read-only và UID/GID local. S3 internal=http://s3:8333, public=http://127.0.0.1:59000.
- [ ] dev.py init tạo keys và .env nếu chưa có; run truyền env bằng subprocess không source shell. Giữ nguyên key/config có sẵn.
- [ ] Make verify: format/vet/unit/build/mod verify; integration/race và security thành target riêng; OpenAPI regen không đổi artifact.
- [ ] CI dùng Go1.26.11, dịch vụ test thật, artifact coverage/testlog, vuln scanner pin v1.9.0; không auto deploy.
- [ ] Docker build, compose config, project riêng up, health/OTP/mua sắm/signed S3 smoke qua network thật, shutdown giữ dữ liệu.
- [ ] Cập nhật lệnh/giới hạn vào báo cáo chung, commit/push F01-runtime-packaging.

Self-review: hai endpoint tránh URL nội bộ không truy cập được; kiểm bí mật và startup health nằm trong test/smoke; CI remote không đánh PASS trước khi có runner result. Load/restore/observability/pagination vẫn theo F37, chưa bị loại khỏi phạm vi.
