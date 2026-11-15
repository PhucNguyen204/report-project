# Queue backlog metrics implementation plan

> **For agentic workers:** TDD và verification-before-completion, ownership tách khỏi accounts. Root giữ runtime wiring, docs/Git và full regression.

**Goal:** đóng phần queue backlog/waiting-age còn thiếu trong F37 để phát hiện email/push/AI bị tồn đọng.

**Architecture:** observer nhận QueueStats callback đọc tổng hợp PostgreSQL; private Prometheus scrape dùng collector có timeout250ms. Callback chỉ đọc trạng thái River/loại event đã giới hạn; không decrypt payload, không chạm API transaction. Không thêm goroutine hoặc gọi network provider.

**Tech Stack:** prometheus/client_golang/pgx/River versions đã pin trong go.mod; không dependency/migration mới.

**Spec:** docs/backend/runbooks/observability.md mục giới hạn queue waiting-age/backlog, F37 plan và private ops hiện có.

## Constraints

- QueueGroup {Kind, State string; Jobs int64; OldestDueSeconds float64}; QueueSnapshot {Groups []QueueGroup}; Options.QueueStats func(context.Context)(QueueSnapshot,error).
- platform.DB.QueueStats(ctx context.Context)(observability.QueueSnapshot,error) đọc duy nhất aggregate query trong river_job. Dùng db.Clock.Now() cho due age. Không query payload hoặc gửi SQLerror/config vào metric/log.
- Labels kind dùng cùng whitelist với worker gồm ai.suggest + other, không queue tên tùy ý hoặc user/space/jobID. Export JobKinds() []string và JobKind(string) string nếu cần reuse whitelist SQL/collector/worker; không expose mutable global slice.
- State chỉ available,pending,retryable,running,scheduled,discarded. Không đếm lịch sử completed/cancelled; dùng existingstate index, không sửa schema River/upstream hoặc migration đã commit.
- Metric dicho_queue_jobs{kind,state}: gauge số job hiện tại, đủ zeros cho bucket vắng. dicho_queue_oldest_due_seconds{kind}: max(now-scheduled_at,0) cho available/retryable/scheduled đã due, không tính future hoặc pending/running. Không gọi đây là SLA end-to-end.
- dicho_queue_stats_up=1 chỉ khi toàn snapshot hợp lệ; DB error/timeout/invalidsnapshot →0 và bỏ count/age samples của lần scrape đó, không dùng số0 giả khỏe hoặc giữ stale snapshot. Handler /metrics vẫn200 để up=0 scrape được.
- Collector timeout250ms trước query, callback DB phải tôn trọng context; promhttp max4inflight giữ nguyên. Không làm fail /healthz hoặc đổi public API khi telemetry query lỗi.

## Review focus

1. Future scheduled và retry chưa due không tăng age; job due cũ nhất qua nhiều state/kind có age đúng.
2. Job hoàn tất/xóa khỏi DB không còn count/age stale; emptyqueue ghi đúngzeros.
3. Unknown eventkind/payload chứa token/tenantID chỉ gộp other, không xuất dữ liệu nhạy cảm.
4. DB unavailable/timeout không treo scrape, không gây500 hoặc falsehealthyzeros; scrape đồng thời race clean.
5. Runtime api/worker/all đều nối callback, privateops metric đúng; API public không lộ metrics; không biến backlog thành readiness gate mới.

### Task1: collector + SQL source

Files: internal/observability/queue.go +queue_test.go; observer.go/worker.go (shared whitelist); internal/platform/queue_stats.go +queue_stats_integration_test.go.

- [x] Unit collector test literal counts/oldest/zeros/other/errors/bad data +concurrent scrape/contextdeadline, quan sát RED trước implementation.
- [x] Implement collector/Options registration +central bounded kind helper. Validate negativecounts/nonfiniteage/negativeage/unknownstate, gộp duplicates an toàn hoặc reject; không partialsamples.
- [x] RealPG fixture encrypted events nhiều states/due/future/terminal; test source SQL aggregate+scrape, rows cập nhật/empty/closedDB/cancellation. Quan sát RED aggregate thiếu/sai trước code SQL.
- [x] Module unit/integration/race/staticcheck và full default suite; báo root files/RED/GREEN.

### Task2: runtime/contract evidence

Files: cmd/server/main.go; app/ops integration test; docs/backend/runbooks/observability.md; PROGRESS.md.

- [ ] Wire Options.QueueStats=db.QueueStats trong runtime. Test privateops với DB source (realPG) và không có callback giữ metric hiện tại, không expose public API.
- [x] Full make verify integration security +secret scan/review. Docker image scrape kiểm up=1/count/age, giữ không publish8081.
- [x] Commit Feature-ID F37-queue-backlog; push GitLab/GitHub feature rồi merge main không force.

Self-review: collector chỉ phản ánh queue retained state tại scrape; không chứng minh mail đến inbox, push tới thiết bị hoặc SLA99%. Monitoring/alert deployment và node filesystem vẫn là hạng mục khác.
