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
| Toolchain Go | PASS | `go version`: go1.26.11 darwin/arm64; nâng sau scanner RED |
| PostgreSQL integration environment | PASS local | PostgreSQL16, SeaweedFS S3, Mailpit đang chạy; Redis7 bổ sung ngày12/11 |
| Test backend/coverage/race/build | LOCAL_PASS13/11 | Go1.26.11 full integration/race exit0; accounts86.1%, inventory87.0%, planning86.8%, media85.8%, exports85.1%, notification90.7%; app80.4%, platform71.3%, kernel95.9% |
| Email/FCM/S3 thật | PARTIAL | SMTP Mailpit + S3 SeaweedFS qua network thật PASS; FCM và production credentials NOT_RUN |
| K1–K5 / load / restore | PARTIAL | K1–K5 network85 HTTP checks PASS; tải và restore còn mở |

## Tiến độ tính năng

| ID | Phạm vi | Trạng thái | Commit/evidence |
|---|---|---|---|
| F01 | Runtime, DB, migrations, harness/CI | IN_PROGRESS | Runtime/worker/OpenAPI local pass; Compose/CI/clean checkout đang hoàn thiện |
| F02 | Quantity/Money/clock/Tx/HTTP | LOCAL_PASS | Kernel95.9%; transaction/context/panic/cancellation có regression |
| F03 | Auth scope/idempotency/request limits | LOCAL_PASS | f6accaf; Redis2 instance40 request chỉ10 được nhận; proxy-prefix regression |
| F04 | Worker/email port | LOCAL_PASS | faa4860; actual River worker, encrypted events và SMTP Mailpit |
| F05 | UC01 đăng ký/OTP | LOCAL_PASS | 6f876c7; full accounts86.1%, OTP/counter/activation có assertions |
| F06 | UC02 phiên/thiết bị | LOCAL_PASS | 6f876c7; rotate/reuse/grace/logout và device scope |
| F07 | UC23 danh mục/đơn vị nền tảng | LOCAL_PASS | 0ab2cd2/5e30ea9; ETag/dimension/canonical locking đã sửa |
| F08 | UC25 cấu hình/audit | LOCAL_PASS | 0ab2cd2; atomic config/audit; đọc live DB từ lần gọi kế tiếp |
| F09 | Media | LOCAL_PASS | f888a7d; actual S3 compatible85.8%; production store chưa smoke |
| F10 | UC04 hồ sơ/nhắc | LOCAL_PASS | 392b536/5e30ea9; profile/reminders/version/media ownership |
| F11 | Inbox/delivery | LOCAL_PASS | 153d0e8; notification90.7%; FCM thật NOT_RUN |
| F12 | UC05 nhóm | LOCAL_PASS | 5e30ea9; nhóm/member và versioned metadata |
| F13 | UC06 mời/join | LOCAL_PASS | 5e30ea9; preview/rotate/join/race chỗ cuối |
| F14 | UC07 rời/xóa/chuyển quyền | LOCAL_PASS | 5e30ea9/2ddf402; leave/remove/transfer cùng cleanup assignment |
| F15 | UC11 nhập tay | LOCAL_PASS | 9644352/2ddf402; nhập tay, lot/movement atomic |
| F16 | UC12 xem/lọc tủ | IN_PROGRESS | 2ddf402; filter/cursor/facet pass; audit default20 còn mở |
| F17 | UC13 sửa/sử dụng/loại bỏ | LOCAL_PASS | d6f45e7/2ddf402; FEFO/stale remaining/metadata merge |
| F18 | UC08 danh sách/mặt hàng/copy/lịch sử | IN_PROGRESS | 2ddf402; workflows pass; keyset list pagination còn mở |
| F19 | UC09 mua/undo | LOCAL_PASS | 2ddf402; purchase/undo/batch rollback/concurrency |
| F20 | UC11 nhập từ mua sắm | LOCAL_PASS | 2ddf402; stock source unique và total cost bảo toàn |
| F21 | UC08 phân công/push | LOCAL_PASS | 2ddf402; role/race/digest5 giây; live FCM chưa smoke |
| F22 | UC15 công thức/yêu thích/copy | IN_PROGRESS | c498d1b/d5dac6a; ảnh/copy/favorite pass; recipe pagination còn mở |
| F23 | UC16 xem/đối chiếu | LOCAL_PASS | c498d1b/d5dac6a; scale/optional/dimension/stock comparison |
| F24 | UC17 lịch | IN_PROGRESS | d39c0d5/d5dac6a; lịch/horizon/cooked rules pass; pagination còn mở |
| F25 | UC10 từ công thức | LOCAL_PASS | 18a04e0; missing recipe preview/confirm |
| F26 | UC10 từ lịch | LOCAL_PASS | 18a04e0; dated allocation và regression0,3kg |
| F27 | UC18 gợi ý | LOCAL_PASS | e8e97be; score/live settings/fallback; tải NOT_RUN |
| F28 | UC19 đã nấu/trừ kho | LOCAL_PASS | d39c0d5/d5dac6a; atomic cook/If-Match/replay |
| F29 | UC14 reminder | LOCAL_PASS | 153d0e8; Tick/reminder/retention pass; SLA vận hành chưa đo |
| F30 | UC21 báo cáo | LOCAL_PASS | df7a33b; report views/previous period/inclusive range |
| F31 | UC22 export | LOCAL_PASS | 96ef09c; exports85.1%, S3 và PDF/CSV thật |
| F32 | UC20 search/suggest | LOCAL_PASS | df7a33b; scoped unaccent search/suggest và bounds |
| F33 | UC23 templates/duyệt/gộp | IN_PROGRESS | 5e30ea9/d5dac6a/f97b6e8; review/template/remap pass; admin pagination còn mở |
| F34 | UC24 quản trị tài khoản | IN_PROGRESS | 5e30ea9; status/role/revoke pass; admin list pagination còn mở |
| F35 | Copy/PurgeSpace/xóa tài khoản | LOCAL_PASS | f97b6e8; copy/purge/anonymization; atomic dispatcher có test |
| F36 | UC03 mật khẩu | LOCAL_PASS | 392b536/0b6cb74; reset/change/retention đúng thời hạn |
| F37 | E2E/security/load/restore/release | IN_PROGRESS | 41caf63; scanner exit0; E2E mở rộng/load/restore/monitoring/CI/review còn mở |

## Checklist chất lượng toàn dự án

- [ ] Đủ25 UC/biến thể và các API hỗ trợ trong scope, mỗi mục có test liên kết.
- [x] Scope authorization được kiểm ở API và DB; admin không đọc dữ liệu gia đình.
- [x] Tiền/lượng/ngày chính xác; transaction/concurrency/idempotency có test PostgreSQL thật.
- [x] TDD có RED/GREEN; unit/HTTP/integration/race/regression pass.
- [ ] Coverage handwritten nghiệp vụ≥85%, core algorithms≥90%; case quan trọng không bỏ sót.
- [x] Worker transactional, dedupe/retry/per-device; không gọi provider trong transaction.
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

### F03 — transaction HTTP đọc được dữ liệu vừa ghi và không rò kết nối — 12/11/2026

Feature-ID: F03-transaction-context.
- [x] Root quan sát behavioral RED: handler nhận500 vì đọc pool khác chưa thấy dữ liệu đang ghi; panic để lại1 kết nối acquired.
- [x] ContextPool dùng lại transaction trong request context cho Query/Exec/QueryRow; DB.Tx luôn rollback khi callback lỗi/panic hoặc commit thất bại.
- [x] Root chạy `go test -tags integration ./internal/platform ./internal/app -count=1 -race`: PASS exit0.
- [x] Đọc sau ghi trả201 đúng trạng thái; panic không commit và không giữ connection; retry phục hồi request body.
- [ ] Full backend đang có task mới trong exports; không dùng kết quả component để khẳng định toàn bộ hệ thống đã pass.

### F22/F23 — công thức, mẫu và sao chép nguyên tử — 12/11/2026

Feature-ID: F22-recipes.
- [x] Root test snapshot algorithms+recipes+templates+recipe/copy tests với `-tags integration -count=1 -race`: PASS exit0 (2.750s).
- [x] Creator/leader, template chỉ đọc/sao chép, optimistic version, favorite và rollback nguyên liệu có test PostgreSQL.
- [x] CopyRecipe ghi origin+recipe+ingredients+activity cùng một transaction; trigger failure không để lại bản sao dở dang.
- [ ] HTTP tổng hợp và ảnh công thức sẽ được commit task riêng; không loại yêu cầu ảnh tùy chọn khỏi phạm vi UC15.

Baseline toàn hệ thống12/11: app78.8%, kernel95.9%, accounts84.9%, inventory86.4%, planning86.5%, media86.7%, exports85.5%, notification47.3%, platform65.3%. Ba test lifecycle media/exports mới đang RED, do đó full suite chưa PASS.

### F01 — khóa dependency cho runtime và kiểm thử contract — 13/11/2026

Feature-ID: F01-runtime-dependencies.
- [x] go.mod/go.sum khóa AWS SDK S3, Redis7 client, OAuth2 FCM, GoPDF và JSON Schema2020-12 validator theo version cụ thể.
- [x] Root `go mod verify`: all modules verified, exit0. `go mod tidy` và buildserver/keygen thực hiện thành công.
- [x] .local privatekeys và bin được gitignore trước khi tạo khóa; helper tạo0600 và không ghi đè khóa đã có.
- [x] Live smoke actual server13/11: health/ready/openapi200, register201, OTP quaSMTPMailpit+kích hoạt200, authenticatedcreate201, idempotentreplay201, read200. Artifact local: output/backend-qa/live-smoke.json.
- [ ] Đây là checkpoint môi trường/runtime; chưa khẳng định toàn bộ releasegate đã hoàn tất.

### F04 — mã hóa payload trong hàng đợi bền vững — 13/11/2026

Feature-ID: F04-encrypted-events.
- [x] EventCipher AES-256-GCM, nonce ngẫu nhiên, xác thực cả eventkind/key; sai key/tamper/bỏ cờ mã hóa bị từ chối.
- [x] Root unit RED thiếu cipher rồi GREEN; actual System+worker+PG test pass đăng ký/kích hoạt với payload được giải mã chỉ ở worker.
- [x] Runtime bắt buộc queue key riêng32byte, độc lập signing key để xoay khóa JWT không làm mất job30ngày; file0600 ngoàiDB/Git.
- [x] PDO/HTTP smoke dùngSMTP thật tớiMailpit, không in OTP/token/mật khẩu; không để OTP hoặc người nhận rõ trong production River args.
- [ ] Fixture NewDB không truyền key vẫn hỗ trợ plain event để kiểm thử từng module; factory ứng dụng đầy đủ luôn yêu cầu key.

### F03 — khóa UUID, logout và hoàn tất media — 13/11/2026

Feature-ID: F03-command-idempotency. Trạng thái LOCAL_PASS.
- [x] Khóa sai UUID bị từ chối trước handler; logout có xác thực bắt buộc khóa.
- [x] Hoàn tất media giữ reservation bền vững nhưng giải phóng transaction trước S3; replay kiểm tra quyền lại và khác object/body trả400.
- [x] Regression RED→GREEN đã quan sát cho UUID, logout và transaction quanh external I/O.
- [x] Root `go test -tags integration ./... -count=1 -race -cover -timeout 5m`: PASS exit0 ngày13/11; platform69.0%, app76.5%; các module nghiệp vụ85.1–90.7%.
- [x] `go vet ./...` và `git diff --check`: PASS.
- [ ] Coverage hạ tầng và release gates chưa hoàn tất; không đánh dấu toàn backend DONE.

### F12–F14 — nhóm, mã mời và chuyển quyền — 13/11/2026

Feature-ID: F12-household. Trạng thái LOCAL_PASS.
- [x] Tạo/xem/sửa nhóm, mã mời có rotate/thu hồi, preview không ghi dữ liệu, một family/user và giới hạn thành viên.
- [x] Tranh chỗ cuối chỉ một người thành công; trưởng nhóm phải chuyển quyền, người cuối giải tán; hook bỏ phân công rollback cùng membership.
- [x] If-Match/ETag và lịch sử trường: PATCH metadata ghi đè từng trường, báo overwrittenFields chính xác; quyết định quyền/status và PUT danh mục dùng version nghiêm ngặt.
- [x] Review sửa ETag danh mục, chặn khác dimension/tự khóa admin, khóa dòng canonical khi kiểm thực phẩm, thông báo email khi admin khóa.
- [x] Root full regression `go test -tags integration ./... -count=1 -race -cover -timeout 5m`: PASS; accounts86.1%. `go vet ./...`: PASS.
- [ ] Copy/purge toàn module và delivery thật được ghi ở task riêng; không suy ra từ test nhóm.

### F16–F21 — API mua sắm và tủ lạnh — 13/11/2026

Feature-ID: F18-shopping-http. Trạng thái LOCAL_PASS.
- [x] Danh sách/mặt hàng, sao chép danh sách, lịch sử, mua/undo và nhập tủ qua batch nguyên tử; nhập lại không tạo lô trùng.
- [x] Giữ lượng/giá trị qua đổi đơn vị, lưu canonical food; quy tắc danh sách completed và lô đã stock có assertion.
- [x] Phân công theo vai trò, bỏ phân công cùng transaction khi rời nhóm, digest5 giây và các đợt sau vẫn được gửi.
- [x] HTTP tủ lạnh có lọc/cursor/facet, scope và version; stale remaining409, metadata merge theo lịch sử trường.
- [x] Root full suite integration/race PASS13/11; inventory87.0%, không test skip. Migration021 bảo vệ dimension đơn vị đang tham chiếu.
- [ ] Phân trang mặc định và toàn bộ endpoint danh sách còn cần audit theo đặc tả default20/max100; không tick toàn hợp đồng đã khớp.

### F09 — ảnh riêng tư qua S3 — 13/11/2026

Feature-ID: F09-private-media. Trạng thái LOCAL_PASS.
- [x] Signed PUT5 phút, JPEG/PNG≤5MB, kiểm MIME/kích thước/giải mã ảnh thật và giới hạn pixel trước hoàn tất.
- [x] Snapshot key bất biến, kiểm lại quyền khi complete/read, signed GET≤15 phút; không public bucket.
- [x] Giữ ảnh còn được gia đình/template tham chiếu khi copy/purge/anonymize; dọn staging/orphan bằng job bền vững.
- [x] Root full integration/race PASS; media85.8%. Test dùng S3 thật SeaweedFS qua AWS SDK và URL ký, gồm upload/download/delete/deny và race thu hồi quyền.
- [ ] Chưa smoke MinIO/AWS production; không gọi fixture S3 tương thích là MinIO thật.

### F24/F28 — lịch bữa ăn và xác nhận đã nấu — 13/11/2026

Feature-ID: F24-meals-cook. Trạng thái LOCAL_PASS core.
- [x] Lịch theo ngày VN/horizon cấu hình; món tự do hoặc công thức, xóa công thức giữ tên món trong lịch.
- [x] Nấu trừ kho FEFO cùng transaction, thiếu kho rollback cả meal/lots; hai request khác key chỉ tiêu thụ một lần.
- [x] Cook dùng version, cooked replay không tiêu thụ thêm; không sửa/xóa món đã nấu.
- [x] Root full integration/race PASS13/11. HTTP If-Match và contract thuộc checkpoint transport tiếp theo.

### F25/F26 — bù nguyên liệu từ công thức và lịch — 13/11/2026

Feature-ID: F26-missing-workflow. Trạng thái LOCAL_PASS core.
- [x] Preview theo servings, ngày nấu/hạn dùng và pending đến kịp; nguồn cung không tính hai lần, regression thiếu0,3kg.
- [x] Confirm giữ lượng/dòng người dùng chọn, kiểm lại food/unit/target và rollback khi batch sai; không ghi trực tiếp schema shopping.
- [x] Root full integration/race PASS13/11; test generate/missing/canonical/precision và các boundary không bị skip.
- [ ] HTTP aggregate/contract được chốt checkpoint riêng.

### F27 — gợi ý từ kho hiện tại — 13/11/2026

Feature-ID: F27-suggestions. Trạng thái LOCAL_PASS core.
- [x] Trọng số R/E/F từ config, ngưỡng/độ phủ lượng/nguyên liệu tùy chọn và ưu tiên sắp hết hạn; điểm0,75 xếp trên0,6.
- [x] Top/tie ổn định, favorite theo user, fallback3, thay đổi kho/cấu hình phản ánh ở lần đọc kế tiếp.
- [x] Root full integration/race PASS13/11; test không dùng cache fake để thay dữ liệu PostgreSQL.
- [ ] Đo tải500 công thức/200 lô và target latency còn NOT_RUN.

### F30/F32 — báo cáo và tìm kiếm — 13/11/2026

Feature-ID: F30-reports-search. Trạng thái LOCAL_PASS core.
- [x] Báo cáo mua sắm/tiêu thụ/lãng phí đọc qua view, bảo toàn lượng/tiền, tách thiếu giá, thống kê danh mục/top/reason và kỳ trước.
- [x] Boundary ngày VN, khoảng ngày bao gồm≤12 tháng, mẫu số0 và định giá lô có regression PostgreSQL.
- [x] Tìm không dấu/đ, q≥2, scope thực phẩm/công thức/lô; không lộ private/template ẩn và chống SQL injection qua tham số.
- [x] Root full integration/race PASS13/11; planning86.8% trên toàn package.
- [ ] Tải và toàn bộ keyset pagination chưa nghiệm thu.

### F22–F28 — HTTP planning và ảnh công thức — 13/11/2026

Feature-ID: F24-planning-http. Trạng thái LOCAL_PASS.
- [x] Đăng ký đầy đủ route recipe/template/favorite/meal/missing/suggestion/report/search; authz/scope/malformed query/body có test router thật.
- [x] Ảnh tùy chọn phải ready, đúng uploader/purpose/target; lỗi rollback cả công thức. Copy tin cậy giữ reference ảnh bất biến.
- [x] M06 bắt buộc If-Match dương, planned stale409 trước trừ kho; cooked replay trả hiện trạng. Test RED→GREEN cho thiếu header/stale đã quan sát.
- [x] Migration031 chặn đổi dimension đơn vị recipe; chấm điểm từ chối NaN.
- [x] Root full integration/race PASS13/11; planning86.8%; contract route/schema được kiểm riêng ở task OpenAPI.
- [ ] Keyset/default pagination toàn endpoint còn mở, chưa tuyên bố khớp toàn bộ phần phân trang của đặc tả.

### F35 — riêng tư, sao chép và vòng đời không gian — 13/11/2026

Feature-ID: F35-space-lifecycle. Trạng thái LOCAL_PASS module.
- [x] Xóa tài khoản kiểm mật khẩu, thu hồi ngay phiên, chặn leader còn thành viên; ẩn danh sau30 ngày, không xóa lịch sử người khác.
- [x] Clone private foods giữ shelf life và ánh xạ alias đã gộp về canonical; copy lists/lots/recipes giữ lượng/giá trị, bỏ trạng thái mua/assignment của nguồn.
- [x] Remap food bảo toàn base quantity, gộp trùng chính xác, idempotent; purge chỉ namespace của space đã giải tán theo thứ tự dependency.
- [x] Root full integration/race PASS13/11 cho accounts/inventory/planning, có scope/rollback/alias và state assertions.
- [ ] Dispatcher cross-module và receipt replay được chốt riêng ở runtime; không dùng module fake thay bằng chứng hệ thống.

### F11/F29 — inbox, delivery và nhắc hạn — 13/11/2026

Feature-ID: F11-notifications. Trạng thái LOCAL_PASS.
- [x] Inbox theo user, cursor default20/max100, đánh dấu đã đọc idempotent, dedupe event và expiry mỗi user/ngày.
- [x] Delivery từng device: retry device lỗi, vô hiệu token hỏng, replay không gửi lại device đã thành công; retention inbox90 ngày/delivery30 ngày không làm fanout lại.
- [x] SMTP MIME Unicode/cancellation và roundtrip Mailpit SMTP/API thật đã chạy; FCM adapter kiểm HTTP contract.
- [x] Root full integration/race PASS13/11; notification90.7%, không skip.
- [ ] FCM thật chưa có credential, SLA99%/15 phút chưa đo trong vận hành; không hứa exactly-once tới provider.

### F31 — xuất CSV/PDF bất đồng bộ — 13/11/2026

Feature-ID: F31-report-exports. Trạng thái LOCAL_PASS.
- [x] pending→ready/failed qua River cùng transaction; requester/member gates, URL15 phút và file7 ngày; retry/orphan/purge có test S3 thật.
- [x] CSV UTF-8 chống formula injection, PDF nhúng NotoSans có giấy phép; PDF tiếng Việt18 trang đã render/kiểm tra ảnh và trích chữ.
- [x] Root phát hiện boundary export khác report: test khoảng01/01/2026–01/01/2027 RED, sửa từ chối ngày cuối bằng mốc12 tháng, GREEN.
- [x] `go test -tags integration ./internal/modules/exports -count=1 -race -cover`: PASS85.1%; full `./...` integration/race chạy lại sau sửa PASS exit0.
- [ ] Live production object store/FCM chưa kiểm chứng. Bài test scheduler retention mới đang được triển khai ở task runtime tiếp theo.

### F36 — dọn dữ liệu xác thực hết thời hạn — 13/11/2026

Feature-ID: F36-credential-retention. Trạng thái LOCAL_PASS.
- [x] OTP hết hạn thêm24 giờ, refresh revoked/expired thêm30 ngày được xóa; giữ reset còn sống và phiên hợp lệ.
- [x] Test RED thiếu Cleanup rồi GREEN; lỗi xóa phiên rollback cả OTP, gọi lại không phá dữ liệu còn hạn.
- [x] Root full integration/race/coverage và vet PASS13/11; accounts86.1%.
- [x] Regression scheduler RED vì inbox/OTP cũ còn lại, đã nối cleanup vào Tick và GREEN; wiring được lưu ở checkpoint runtime kế tiếp.

### F37 — nâng bản vá Go và dependency bảo mật — 13/11/2026

Feature-ID: F37-security-toolchain. Trạng thái LOCAL_PASS scanner.
- [x] govulncheck v1.9.0 trên Go1.26.6 báo11 lỗ hổng stdlib có call trace, thêm cảnh báo module x/crypto chưa gọi; lưu JSON tại output/backend-qa/govulncheck-before.json.
- [x] Xác minh [Go release history](https://go.dev/doc/devel/release):1.26.11 phát hành10/11/2026. Nâng minimum go.mod lên1.26.11 và x/crypto0.59.0 (kèm sync/sys/text tương thích).
- [x] `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`: exit0, No vulnerabilities found. Không diễn giải thành bằng chứng hệ thống không còn mọi lỗi bảo mật.
- [x] Go1.26.11 full integration/race/coverage PASS exit0; `go mod verify` all modules verified; build server PASS.
- [ ] Secret/container scan, tải, restore, CI remote và review release vẫn còn mở.

### F01/F03/F04 — ứng dụng API/worker và hợp đồng OpenAPI — 13/11/2026

Feature-ID: F01-system-runtime. Trạng thái LOCAL_PASS runtime, release IN_PROGRESS.
- [x] Binary server/keygen, private keys0600, cấu hình development/production, API/worker/all, startup DB/Redis/S3 và graceful shutdown.
- [x] Nối đủ module, route, encrypted River worker, atomic cross-module copy/remap/purge receipts; Tick có durable slot và dọn inbox/OTP/session/media/export/idempotency.
- [x] HTTP app version/body/MIME/request ID/no-store, rate GCRA Redis nguyên tử; lỗi Redis fallback bộ nhớ có trạng thái degraded.
- [x] Root quan sát RED proxy-prefix thay đổi qua trusted chain giúp request11 né quota; sửa đọc chuỗi từ proxy tin cậy về client đầu tiên không tin cậy, GREEN.
- [x] OpenAPI3.1 có74 paths/101 operations/198 schemas,86 mã API trong scope; official schema/ref validation, registered mux và HTTP request/response thật khớp. UUID contract RED→GREEN.
- [x] Go1.26.11 `go test -tags integration ./... -count=1 -race -cover -timeout 5m` PASS exit0; build/vet/mod verify PASS. Scanner No vulnerabilities found.
- [ ] Phân trang ở một số endpoint, packaging/CI/load/restore/observability và FCM thật chưa hoàn tất. Bộ test contract hiện tại không thay thế review đủ mọi biến thể UC.

### F09 — URL S3 công khai cho client — 13/11/2026

Feature-ID: F09-public-presigner. Trạng thái LOCAL_PASS.
- [x] Test RED thiếu public presigner và config bỏ qua endpoint không hợp lệ, GREEN sau sửa.
- [x] WithPublicClient trả store mới: ký upload/download với host public, I/O vẫn đến internal endpoint; ký không gọi network và không đổi base store.
- [x] S3_PUBLIC_ENDPOINT mặc định S3_ENDPOINT; production bắt cả hai HTTPS. Runtime nối cùng credentials/region, không sửa host sau ký.
- [x] Full integration/race/coverage PASS13/11; media85.9%, app81.0%; bộ test S3 thật cũ vẫn pass.
- [ ] Smoke qua API trong container và chữ ký được S3 thật chấp nhận từ host thuộc packaging task tiếp theo.

### F01/F37 — Docker, Make và cấu hình GitLab CI — 13/11/2026

Feature-ID: F01-runtime-packaging. Trạng thái LOCAL_PASS packaging; CI remote NOT_RUN.
- [x] Docker image Go1.26.11/Alpine khóa digest,48MB, nonroot, runtime CA/tzdata; `.env`/private keys không vào build context/Git.
- [x] Compose PostgreSQL/Redis/SeaweedFS/Mailpit/app có healthcheck, bind secrets read-only, rootfs read-only, drop capabilities; project dicho-qa và cổng riêng, không đụng fixture cũ.
- [x] Init giữ key/config đã có; Make verify/openapi/integration/security/secrets/build/run/up/down/smoke và README có lệnh cụ thể.
- [x] Fixture hỗ trợ TEST_DATABASE_URL/TEST_REDIS_URL/TEST_S3_ENDPOINT/TEST_SMTP_ADDR/TEST_MAILPIT_URL; test chạy trên project/port riêng.
- [x] macOS `make verify openapi`, `make integration`: PASS330 case/0 fail/0 skip. Linux runner local lần đầu FAIL TestPDFEmbedsVietnameseWrapsAndPaginates do thiếu Poppler; thêm poppler-utils/tzdata vào CI, rerun toàn bộ330 PASS.
- [x] Docker app health PASS; `scripts/smoke.py` qua network thật K1–K5 PASS85 HTTP checks, OTP từ Mailpit, signed S3 PUT/complete/GET từ host, CSV/PDF qua worker. Mua20000đ/tiêu thụ12000đ/lãng phí2000đ đúng assertion. Artifact output/backend-qa/compose-smoke.json.
- [x] govulncheck No vulnerabilities found. Gitleaks v8.30.1 history66 commits phát hiện4 ví dụ LaTeX có dấu `...`; review đúng từng giá trị rút gọn và ignore đúng4 fingerprint, rerun exit0. Staged diff39068 bytes scan exit0; không blanket-exclude file/rule.
- [x] CI YAML parsed, job Linux dùng cùng Go image/dịch vụ thật, artifacts14 ngày; không auto deploy.
- [ ] Remote runner, fresh Git clone smoke, tải/restore/monitoring, pagination và production FCM chưa hoàn tất. Không tick production release hay SLA từ kết quả local.

### F01 — kiểm chứng bản clone mới từ GitLab — 13/11/2026

Feature-ID: F01-fresh-checkout. Code checkpoint04f3203.
- [x] Clone SSH mới nhánh baocao-phucnn từ GitLab, dùng SSH configuration hiện có; lần clone đầu thiếu ProxyCommand bị timeout, retry đúng cấu hình thành công.
- [x] Trong checkout trống: make init verify openapi PASS; key mới0600, không cần file chưa commit trong workspace cũ.
- [x] make up build lại image, project dicho-fresh/cổng/volume riêng; K1–K5 network PASS84 HTTP checks, SMTP/S3/CSV/PDF. make down graceful PASS, giữ volume.
- [x] Artifact output/backend-qa/fresh-clone-smoke.json. Số request polling khác vòng trước là bình thường; không đếm mỗi poll thành một requirement riêng.
- [ ] GitLab pipeline API trả403 khi đọc không có token; CI remote chưa được xác nhận. Linux job tương đương chạy local đã PASS330 case.

### F03 — từ chối JSON null trước khi sửa hồ sơ — 13/11/2026

Feature-ID: F03-json-object. Trạng thái LOCAL_PASS.
- [x] Root RED: PATCH/me body null trả200, có thể tăng version/phát activity dù input không đúng object.
- [x] Decoder kiểm root object, giữ strict unknown field, trailing JSON và UseNumber; null/primitive/array bị400 trước nghiệp vụ.
- [x] Regression xác nhận không đổi version và không tạo member.updated. Unit decoder RED→GREEN.
- [x] make verify integration PASS331 case/0 fail/0 skip, có race và coverage; build/vet/mod verify PASS.
- [x] Core coverage từ profile trước: Quantity95.9%, Allocate FEFO94.7%, Missing95.0%, Score97.7%. Nghiệp vụ từng module≥85%; app/platform chưa đạt85%, giữ gate coverage toàn handwritten mở.

### F37 — merge checkpoint backend vào nhánh chính baocao — 13/11/2026

Feature-ID: F37-main-merge. Theo yêu cầu trực tiếp của người dùng: push code rồi merge nhánh chính.
- [x] Xác minh origin/HEAD là baocao, head d87eb22; feature checkpoint705de4b đã push.
- [x] Tạo worktree độc lập từ origin/baocao, merge --no-ff --no-commit checkpoint705de4b, không có conflict; không đưa pagination đang làm vào snapshot này.
- [x] Trên kết quả merge: make verify integration PASS331 case/0 fail/0 skip, unit/race/vet/build/mod verify đạt. Evidence output/backend-qa/main-merge-integration.jsonl ở workspace chính.
- [x] Backend local và fresh clone/K1–K5 đã có bằng chứng ở các entry trước. Merge không có nghĩa release production đã hoàn tất.
- [ ] Pagination/load/restore/monitoring/FCM thật/CI remote còn mở; GitLab pipeline API đọc không token trả403.
