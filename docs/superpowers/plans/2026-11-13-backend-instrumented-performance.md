# Instrumented performance verification plan

> **For agentic workers:** root triển khai/check/Git với TDD và verification-before-completion.

**Goal:** harness50RPS đo cả HTTP observer hiện đã bật ở runtime, có log JSON và metrics xác nhận không bỏ mẫu.

**Architecture:** giữ dataset/auth/Redis/TCP/open-loop/latency basis của31ad147, inject observer với SDK provider10% root/noexport giống default runtime; log ghi file0600 trong output. Warmup và mọi measured response phải có X-Trace-ID chuẩn. Sau drain scrape registry và đối chiếu375measured+16warmup mỗiroute; request/worker metrics không làm bypass auth/quota.

**Tech Stack:** Go1.26.11/performance tag, PostgreSQL/Redis/S3 thật, existing observer.

**Spec:** NFR09/20 và F37 original plan; observability plan13/11.

## Global constraints

- Không chạy build/suite khác cùng30s measured window; không claim TLS/4G/production SLA hoặc collector/network export overhead.
- Không có token/userID/rawpath trong log; chỉ routes/status/request_id/trace_id, giữ giới hạn mỗiuser120/min.
- Giữ toànsamples gồm error/drop/dispatchlag, không chỉ lấy successes.

## Review focus

1. HTTP observer quên inject phải làm warmup fail vì thiếu trace header.
2. Logs/metrics phải ghi đủ samples dù SDK không sample mọi trace.
3. File/providergoroutine/worker/server phải đóng trước cleanup DB/bucket/Redis.
4. Metric count phải375measured+16warmup mỗiroute, không vô tình include riêng authwarmup hoặc tampercase.
5. SourceHEAD/dirtyfiles/env/limits vẫn ghi rõ; không gán số mới cho snapshot cũ.

### Task1: traced and measured workload

Files: internal/app/performance_integration_test.go, runbooks/performance.md, PROGRESS.md.

- [x] RED warmup reject missingtrace sau khi thêm assertion vào current harness; giữ RED artifact.
- [x] Inject provider/logger/observer, scrape counters sau drain và assert391/route; ghi artifacts và cleanup; campaign GREEN1500requests/0errors.
- [x] Review/campaign và lint/unit/integration/security snapshot trước authz fix đạt; local experiment có giới hạn nêu rõ, xem evidence bên dưới.
- [x] Root chạy full regression sau HIGH bản sửa quyền; 548Go/0fail/0skip +7Python/global85.72%, security No vulnerabilities found; codefix đã commit50377e0. Campaign mới trên code này PASS, không dùng snapshot trước fix thay gate mới.
- [ ] Root chốt commit/push/main cho checkpoint performance; agent không thao tác Git.

Evidence campaign13/11: RED warmup status200/trace_context_missing, giữ performance-observer-missing-red.json. GREEN1500requests/30s/0errors/0drops/0Redisfallback; p95 lots5.243/recipes13.178/suggestions36.377/autocomplete8.166ms. Counter/histogram391/route vàactive0; file0600 có1581HTTP log entries=1500measured+80warmup+1tamper401, mọi entry có trace hex32/request_id và chỉfields cho phép. Artifact source da65035 +dirtyharness/go.mod/plan, không gọi đây là HEAD-only proof. DB dropped/32Rediskeys removed/0remaining/S3bucket deleted.

Review14/11: lần full đầu unit/lint/vet/build đạt nhưng integration513pass/9fail; jsonl walltime01:30→05:24local có17gap75–1049s, trong khi4packagefail elapsed21–63s (max toàn suite123.21s). Giữ RED artifacts instrumented-performance-integration-failed-20261114.jsonl/coverage-failed-20261114.out; không sửaclock/TTL/fixtures. Khi host/Go/PG đồng bộ, focused9case GREEN. `caffeinate -i make verify integration security` trên snapshot trước bản sửa quyền; 522Go/0fail/0skip +7Python, global85.90/app89.19/platform87.03/observer94.07, lint2buildsets/race/vet/modverify/build vàgovulncheck No vulnerabilities found. Stable artifacts instrumented-performance-{integration,coverage,coverage-summary}-stable-20261114.*. HIGH actor state race do reviewer khác tìm thấy đang được root sửa; final regression/Git vẫn pending.

Post-fix14/11: root đã kiểm toàn suite548Go/0fail/0skip +7Python/global85.72%, securityNoVulns trên actor/session fix50377e0. Agent chạy lại `caffeinate -disu make performance` trên50377e0+dirtyharness/go.mod/plan/runbook, không chạy thêm suite nặng trong30s đo; PASS1500requests/0errors/0drops/0Redisfallback, p95 lots4.311/recipes12.244/suggestions35.562/autocomplete7.563ms, max7.205/23.670/46.628/18.252ms. Counter/histogram391/route/active0; log0600 đủ1581HTTP entries/tracehex32/request_id/allowedfields, cleanup DB/32Rediskeys/0remaining/S3 đạt. Artifact cũ giữ performance-before-actor-guard-da650351.json; kết quả mới performance.json và performance-after-actor-guard-50377e0.json, verified_head_only=false. LOCAL_PASS quality evidence; Git checkpoint performance còn root chốt, independent release audit/CI remote không được suy ra từ bài đo.
