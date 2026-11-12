# Báo cáo triển khai backend — Đi chợ tiện lợi

Ngày bắt đầu: 09/11/2026. Nguồn đặc tả: commit `d87eb22`. Lộ trình: [backend plan](../superpowers/plans/2026-11-09-backend.md).

## Quy tắc ghi nhận

Đây là file tổng hợp duy nhất cho kết quả triển khai. NOT_STARTED: chưa làm; IN_PROGRESS: đang code; LOCAL_PASS: kiểm tra local đạt và có commit; DONE: toàn bộ gate áp dụng đạt, CI/evidence đầy đủ; BLOCKED: thiếu điều kiện cụ thể; NOT_RUN: chưa chạy phép kiểm tra. Không dùng kết quả fake adapter để khẳng định dịch vụ thật hoạt động.

Mỗi tính năng phải có entry chi tiết theo mẫu cuối file. Tick chỉ sau khi kiểm tra bằng chứng. Báo cáo đi cùng commit tính năng; SHA của commit chứa entry được truy bằng trailer Feature-ID, rồi cập nhật SHA ở lần ghi tiếp theo. CI test SHA code; cập nhật riêng báo cáo không thay đổi code đã kiểm chứng.

## Quyết định triển khai

- Code tại `backend/`, cùng repo hiện tại. Dùng Go/PostgreSQL theo đặc tả; chia sub-agent theo module, root tích hợp và giữ quyền commit.
- Dùng ba skill upstream Superpowers: writing-plans, test-driven-development, verification-before-completion; không tạo một skill `code` tự viết.
- Nguồn clone: https://github.com/obra/superpowers ; SHA `22f0b463e7a5e579904dd6211a520321035526f7`. Cài tại `~/.codex/skills/`, đã đọc SKILL.md.
- ADR01–08 trong plan là quyết định xử lý mâu thuẫn; không sửa âm thầm nội dung LaTeX.
- Scope gồm25 UC/88 mã API; Y01/Y02 ngoài scope, P08 đã bỏ; frontend-only NFR không tick thay client.

## Tổng quan bằng chứng hiện tại

| Hạng mục | Trạng thái | Bằng chứng/giới hạn |
|---|---|---|
| Đọc phạm vi25 UC và kiểm tra thiết kế | PASS | AGENTS.md, chương1–3, phụ lục; audit độc lập từ sub-agent spec_audit |
| Clone/cài skill coding upstream | PASS | Installer trả installed cho3 skill; clone SHA nêu trên |
| Plan và ma trận kiểm thử | PASS | spec_audit chấp nhận sau sửa dependency/auth, activity log, contracts, River, precision, release gate |
| Toolchain Go | PASS | `go version`: go1.26.6 darwin/arm64 |
| PostgreSQL integration environment | PASS local | PostgreSQL16, SeaweedFS S3, Mailpit đang chạy; Redis7 bổ sung ngày12/11 |
| Test backend/coverage/race/build | IN_PROGRESS | Baseline12/11: kernel95.9%, accounts60.4%, inventory85%, notification47.3%; media/planning còn task chưa hoàn tất lúc chạy baseline |
| Email/FCM/S3 thật | NOT_RUN | Chưa xác minh credential/staging; sẽ ghi riêng adapter fake và smoke thật |
| Load/backup-restore | NOT_RUN | Chưa có hệ thống chạy để đo |

## Tiến độ tính năng

| ID | Phạm vi | Trạng thái | Commit/evidence |
|---|---|---|---|
| F01 | Runtime, DB, migrations, harness/CI | IN_PROGRESS | Migrations/checksum và isolated PG test harness có; runtime/CI tiếp tục |
| F02 | Quantity/Money/clock/Tx/HTTP | IN_PROGRESS | Quantity core: race PASS, coverage95.9%; Tx/HTTP đang tích hợp |
| F03 | Auth scope/idempotency/request limits | IN_PROGRESS | PostgreSQL replay/concurrency/rollback5 tests platform PASS; full router chưa tích hợp |
| F04 | Worker/email port | IN_PROGRESS | River InsertTx rollback/dedupe PASS; provider/worker tiếp tục |
| F05 | UC01 đăng ký/OTP | LOCAL_PASS | Auth core7 tests; coverage68.3%, thiếu ma trận còn mở |
| F06 | UC02 phiên/thiết bị | LOCAL_PASS | Refresh/reuse/logout counters PG+race PASS |
| F07 | UC23 danh mục/đơn vị nền tảng | LOCAL_PASS baseline | Scope/unit/privatefood PG+race PASS; ETag/dimension review fixes tiếp tục |
| F08 | UC25 cấu hình/audit | LOCAL_PASS baseline | Atomic config/audit tests PASS; full negative coverage tiếp tục |
| F09 | Media | NOT_STARTED | — |
| F10 | UC04 hồ sơ/nhắc | LOCAL_PASS | Snapshot profile/password PG+race PASS; E2E và coverage toàn accounts tiếp tục |
| F11 | Inbox/delivery | NOT_STARTED | — |
| F12 | UC05 nhóm | NOT_STARTED | — |
| F13 | UC06 mời/join | NOT_STARTED | — |
| F14 | UC07 rời/xóa/chuyển quyền | NOT_STARTED | — |
| F15 | UC11 nhập tay | LOCAL_PASS core | PostgreSQL+race69.7% phần pantry; HTTP và coverage gate tiếp tục |
| F16 | UC12 xem/lọc tủ | NOT_STARTED | — |
| F17 | UC13 sửa/sử dụng/loại bỏ | NOT_STARTED | — |
| F18 | UC08 danh sách/mặt hàng/copy/lịch sử | NOT_STARTED | — |
| F19 | UC09 mua/undo | NOT_STARTED | — |
| F20 | UC11 nhập từ mua sắm | NOT_STARTED | — |
| F21 | UC08 phân công/push | NOT_STARTED | — |
| F22 | UC15 công thức/yêu thích/copy | NOT_STARTED | — |
| F23 | UC16 xem/đối chiếu | NOT_STARTED | — |
| F24 | UC17 lịch | NOT_STARTED | — |
| F25 | UC10 từ công thức | NOT_STARTED | — |
| F26 | UC10 từ lịch | NOT_STARTED | — |
| F27 | UC18 gợi ý | NOT_STARTED | — |
| F28 | UC19 đã nấu/trừ kho | NOT_STARTED | — |
| F29 | UC14 reminder | NOT_STARTED | — |
| F30 | UC21 báo cáo | NOT_STARTED | — |
| F31 | UC22 export | NOT_STARTED | — |
| F32 | UC20 search/suggest | NOT_STARTED | — |
| F33 | UC23 templates/duyệt/gộp | NOT_STARTED | — |
| F34 | UC24 quản trị tài khoản | NOT_STARTED | — |
| F35 | Copy/PurgeSpace/xóa tài khoản | NOT_STARTED | — |
| F36 | UC03 mật khẩu | LOCAL_PASS | Reset một lần, đổi mật khẩu/thu hồi phiên đã có test PG; external email tiếp tục |
| F37 | E2E/security/load/restore/release | NOT_STARTED | — |

## Checklist chất lượng toàn dự án

- [ ] Đủ25 UC/biến thể và các API hỗ trợ trong scope, mỗi mục có test liên kết.
- [ ] Scope authorization được kiểm ở API và DB; admin không đọc dữ liệu gia đình.
- [ ] Tiền/lượng/ngày chính xác; transaction/concurrency/idempotency có test PostgreSQL thật.
- [ ] TDD có RED/GREEN; unit/HTTP/integration/race/regression pass.
- [ ] Coverage handwritten nghiệp vụ≥85%, core algorithms≥90%; case quan trọng không bỏ sót.
- [ ] Worker transactional, dedupe/retry/per-device; không gọi provider trong transaction.
- [ ] Migration/seed/fresh checkout/OpenAPI khớp implementation.
- [ ] Format/vet/lint/build và dependency/secret scan đạt.
- [ ] K1–K5 E2E, external smoke, load và backup restore có kết quả thực.
- [ ] Comment chỉ ngắn gọn vai trò function; không placeholder endpoint thành công.
- [ ] Mỗi tính năng có commit và entry báo cáo.
- [ ] Không lỗi Critical/Important chưa xử lý sau review độc lập.

## Nhật ký review

### 09/11/2026 — audit đặc tả trước code

Reviewer: sub-agent `spec_audit` (read-only). Phát hiện và đã đưa vào plan:88 mã API sau bỏ P08; search pg_trgm đủ bản đầu; security counter phải commit khi trả lỗi; logout phải kiểm family revocation; scope idempotency theo user; transaction nhập tủ/nấu/rời nhóm; mâu thuẫn BR08/S14/X-Client-Time. Quyết định chi tiết: ADR01–08 trong plan.

Plan review: ACCEPT sau các chỉnh sửa đã ghi. Code review: chưa thực hiện; bắt đầu F01/F02. Root phải cung cấp contracts biên dịch trước giao agent.

## Mẫu entry cho một tính năng

```markdown
### Fxx — tên tính năng — ngày
Trạng thái: IN_PROGRESS | LOCAL_PASS | DONE | BLOCKED
UC/API/BR/NFR: ...
Commit: trailer Feature-ID: Fxx; SHA bổ sung sau commit
File thay đổi: ...
Hành vi nhận được: ...

- [ ] Contract và điều kiện chấp nhận khớp đặc tả
- [ ] Unit test RED rồi GREEN; có lỗi/biên
- [ ] HTTP/contract và authorization pass
- [ ] Integration DB thật và rollback/concurrency pass (hoặc N/A + lý do)
- [ ] Regression toàn backend và race pass
- [ ] Coverage mục tiêu đạt hoặc nêu chính xác gap
- [ ] Format/vet/lint/build/migration/codegen phù hợp pass
- [ ] Review diff và comment ngắn đạt
- [ ] Commit code+test+migration+báo cáo

| Lệnh | Kết quả/exit | Pass/fail/skip | Artifact/CI |
|---|---|---|---|
| lệnh thực tế | NOT_RUN | — | — |

Review findings và cách xử lý: ...
Quyết định khác plan (Ruling), lý do và tác động: ...
Giới hạn/chưa kiểm chứng: ...
Bước tiếp theo: ...
```

### F02 — Quantity chính xác — 09/11/2026

Trạng thái: LOCAL_PASS cho phần Quantity; F02 tổng thể đang làm. Commit truy bằng `Feature-ID: F02-quantity`.

- [x] Test exact0.1+0.2, quy đổi, dấu, storage/JSON, input sai và overflow.
- [x] Regression subscale quan sát RED `silently truncated product`, sửa Mul/Div từ chối mất precision, GREEN.
- [x] `go test ./internal/kernel ./internal/platform -race -count=1 -cover`: exit0; kernel95.9%, platform12.4%.
- [ ] Phần transaction/HTTP và integration toàn hệ thống hoàn tất.

Ghi nhận: RED đầu tiên thiếu symbol; regression precision có behavioral RED thực. Không khẳng định coverage platform đạt gate; integration tests được ghi riêng sau.

### F01/F03/F04 — nền tảng transaction/request/queue — 09/11/2026

Trạng thái: LOCAL_PASS cho foundation; các tính năng tổng thể còn tích hợp. Feature-ID: F01-platform.

- [x] Migrations embedded có checksum, advisory lock, chạy trong transaction.
- [x] Test DB tạo database riêng, cleanup rõ ràng; PostgreSQL16 Docker thật.
- [x] `go test -tags integration ./internal/platform -count=1 -v`:5 tests PASS, exit0.
- [x] Hai request cùng key chỉ ghi một lần; khác body bị chặn; business error rollback cả counter và key.
- [x] River InsertTx không tạo job khi rollback; cùng unique key chỉ một job.
- [ ] Middleware đầy đủ/rate limit/provider/CI và E2E tích hợp.

Review lưu ý: public auth loại khỏi transaction idempotency để counter lỗi được commit; nested DB.Tx dùng transaction trong request context.

### F05/F07 — schema tài khoản, không gian, danh mục — 09/11/2026

Trạng thái: LOCAL_PASS cho migration nền tảng; chức năng API đang triển khai. Feature-ID: F05-schema.

- [x] Migration010 chạy trên PostgreSQL16 mới qua `testutil.NewDB`.
- [x] `go test -tags integration ./internal/platform -count=1`: PASS, exit0.
- [x] Constraint email duy nhất, membership/leader và catalog seed đã được sử dụng trong test thật.
- [ ] API tài khoản/nhóm/danh mục hoàn tất toàn bộ; chưa tick thay phần service.

Push nền tảng trước đó: GitLab nhánh baocao-phucnn đã nhận `cccce67`. Từ yêu cầu mới, root push mỗi commit tính năng sau verify.

### F15/F17 — pantry core, FEFO và nhập tay — 09/11/2026

Trạng thái: LOCAL_PASS core, HTTP và ma trận toàn bộ đang bổ sung. Feature-ID: F15-pantry-core.

- [x] Test input/date/quantity/scope và lot+stock_in movement thật.
- [x] FEFO thứ tự hạn dùng, không tự trừ lô quá hạn, thiếu nguồn rollback nhiều thực phẩm.
- [x] Root chạy snapshot file độc lập: `go test -tags integration internal/modules/inventory/domain.go internal/modules/inventory/service.go internal/modules/inventory/domain_test.go internal/modules/inventory/fixture_test.go internal/modules/inventory/pantry_integration_test.go -count=1 -race -cover`: PASS exit0; coverage69.7%.
- [x] Sub-agent domain RED thiếu symbols rồi GREEN; FEFO domain94.7%, expiry100%.
- [ ] HTTP read/filter/update/discard, full E2E và coverage nghiệp vụ≥85% chưa hoàn tất; không gọi là UC11/UC13 DONE.

### F17 — sửa precision khi trừ lô khác đơn vị — 09/11/2026

Trạng thái: LOCAL_PASS. Feature-ID: F17-unit-normalization.

Review phát hiện1kg−1g bị từ chối dù999g biểu diễn được.
- [x] Sub-agent quan sát behavioral RED cho test1kg−1g và merge1g vào1kg.
- [x] Pantry chuyển lô sang đơn vị cơ sở khi cần, giữ nguyên lượng ban đầu1000g để bảo toàn định giá.
- [x] Root chạy snapshot pantry với `-tags integration -count=1 -race`: PASS exit0.
- [ ] HTTP/mua sắm merge thuộc commit tính năng tiếp theo, chưa khẳng định toàn module hoàn tất.

### F26/F27 — thuật toán phần thiếu và chấm điểm — 09/11/2026

Trạng thái: LOCAL_PASS core; API/cấu hình/coverage toàn feature tiếp tục. Feature-ID: F26-algorithms.

- [x] Test nguồn cung chỉ dùng một lần và còn hạn tại ngày nấu, regression đúng0,3kg.
- [x] Test sai dimension/hàng tới muộn/input âm/overflow, required đủ lượng và optional.
- [x] Ví dụ điểm0,75 cao hơn0,6; boundary công thức/ngày lịch.
- [x] Root chạy `go test internal/modules/planning/algorithms.go internal/modules/planning/algorithms_test.go -count=1 -race -cover`: PASS exit0; coverage89.1%.
- [ ] Mục tiêu core≥90% chưa đạt; bổ sung case có ý nghĩa ở vòng test tiếp theo.

### F05/F06 — xác thực, OTP và phiên — 09/11/2026

Trạng thái: LOCAL_PASS, cần tăng coverage/ma trận trước release. Feature-ID: F05-auth.

- [x] Argon2id, JWT EdDSA, OTP không trả trong response; kích hoạt tạo đúng một personal space.
- [x] Counter5 lần sai được lưu khi trả lỗi; resend/lock/expiry; refresh rotate/grace/reuse và thu hồiJWT.
- [x] Logout family hiện tại, thiết bị khác vẫn có phiên; không lưu refresh token rõ.
- [x] Root chạy snapshot4 files auth với `-tags integration -count=1 -race -cover`: PASS exit0, coverage68.3%.
- [ ] Coverage≥85%, full router/E2E và external email smoke chưa đạt; ghi rõ chưa DONE.

### F04/F21 — không chặn các đợt phân công tương lai — 09/11/2026

Feature-ID: F21-digest-dedupe. Review phát hiện River mặc định giữ completed trong unique states, làm đợt phân công sau bị mất.
- [x] Root quan sát RED platform `bursts1` và inventory `jobs1`.
- [x] Chỉ assignment.digest loại completed/cancelled khỏi uniqueness; các key idempotent khác giữ chính sách cũ.
- [x] `go test -tags integration ./internal/platform -count=1 -race` PASS và regression inventory future-burst PASS.
- [ ] FCM thật và release gate thông báo vẫn chưa đo; không hứa exactly-once tới provider.

### F07/F08 — danh mục dùng chung, cấu hình và audit — 09/11/2026

Feature-ID: F07-catalog-config. Trạng thái LOCAL_PASS baseline.
- [x] ValidateFood và CreatePrivateFood qua port, không cho inventory ghi schema catalog.
- [x] Catalog/admin được đăng ký tách khỏi auth; thay đổi cấu hình validate toàn batch và audit.
- [x] Root test snapshot auth+catalog+admin với `-tags integration -count=1 -race`: PASS exit0.
- [ ] Review còn mở: ETag danh mục phải thay đổi khi sửa, chặn food/đơn vị khác dimension, D02 không tự khóa. Fix đã có test tái hiện ở vòng tiếp theo.
- [ ] Toàn accounts coverage hiện57.1%, chưa đạt gate85%.

### F03 — preview và đổi mật khẩu không cache token — 09/11/2026

Feature-ID: F03-preview.
- [x] Test preview dryRuntrue tạiG07/S15/R08 quan sát RED400, sửa allowlist, GREEN.
- [x] Preview vẫn đi qua xác thực và kiểm tra scope service; chỉ bỏ yêu cầu/caching Idempotency-Key.
- [x] PUT/me/password không cache phản hồi chứa refresh token; counter sai mật khẩu được commit theo handler.
- [x] `go test -tags integration ./internal/platform -count=1 -race`: PASS exit0.
- [x] Bổ sung port LotSearchPort để query lô qua chủ sở hữuinventory.

ADR10: miễn idempotency cache cho thay đổi mật khẩu để không lưu refresh token rõ; client mất response có thể đăng nhập bằng mật khẩu mới.

### F10/F36 — hồ sơ, thiết lập và mật khẩu — 12/11/2026

Feature-ID: F10-profile-password. Trạng thái LOCAL_PASS.
- [x] Root chạy snapshot auth+profile+tests với PostgreSQL thật, `-tags integration -count=1 -race`: PASS exit0 (7.637s).
- [x] Hồ sơ/locale/ngưỡng nhắc1–7/null, mật khẩu hiện tại, reset-token dùngmột lần và thu hồi phiên khác có assertion.
- [x] Không đưa token vào idempotency cache cho PUT/me/password.
- [ ] Toàn accounts chưa đạt85% coverage và API tích hợp còn đang hoàn thiện; không gọi toàn bộ UC DONE.
