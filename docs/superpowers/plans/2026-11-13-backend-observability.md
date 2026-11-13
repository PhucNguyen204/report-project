# Backend observability implementation plan

> **For agentic workers:** triển khai từng task với test-driven-development và verification-before-completion. User đã chọn dùng sub-agent; các agent hiện hết quota đến21:54, root tiếp tục phần độc lập, giữ review độc lập là gate mở.

**Goal:** có số đo RED, log JSON an toàn và OpenTelemetry trace xuyên API/River, qua cổng vận hành riêng.

**Architecture:** observer được inject, không dùng global registry/provider. Route labels lấy pattern đăng ký, method bị giới hạn; không lưu path/query/header/body/tenant ID. TraceContext được lưu thêm trong event ngoài payload AES, chỉ dùng correlation, không quyết định quyền. Job cũ thiếu context vẫn chạy. Metrics chỉ đăng ký trên ops mux.

**Tech Stack:** Go1.26.11, prometheus/client_golang1.25.0, OpenTelemetry1.47.0 (đã kiểm module proxy13/11), OTLP/HTTP khi có endpoint cấu hình.

**Spec:** chapters/c3/trien-khai.tex:75, tong-quan.tex:72, api.tex:31/257; docs/superpowers/plans/2026-11-09-backend.md F01/F37.

## Global constraints

- Không payload/token/email/URL thật trong telemetry; không gửi ra collector nếu chưa cấu hình.
- Không sửa migration đã commit, không đưa network telemetry vào database transaction.
- Metrics không public qua API; ops production bind phải do người triển khai đặt trong private network. Compose không publish ops port.
- TraceContext mới không đổi AAD của ciphertext hiện có; trace metadata không phải nguồn auth hoặc unique key.
- Không claim availability/reminder SLA từ local run; Prometheus/Alloy/Loki/Tempo production deployment là gate riêng.

## Review focus

1. Request auth/version/size fail trước mux vẫn có metric/log với route pattern, không raw ID/query.
2. Request vô danh/method tùy ý không tạo series theo input, panic hoặc WriteHeader nhiều lần không sai status.
3. Concurrent requests/scrapes và worker retries không race, active gauges trở0 sau error/panic.
4. Context qua queue vẫn cùng trace, mỗi attempt có span riêng; job cũ/cipher hỏng được ghi lỗi tổng quát, không lộ payload.
5. Collector lỗi/cấu hình sai không giữ shutdown vô hạn; tắt exporter phải không phát HTTP telemetry.

### Task1: observer and bounded telemetry

Files: internal/observability/{observer.go,http.go,worker.go,*_test.go}; go.mod/go.sum; scripts/coverage_report.py.

Interfaces: New(Options{Logger,TracerProvider,PoolStats})->*Observer; HTTP(*http.ServeMux,http.Handler)->http.Handler; Work(ctx,kind,attempt,func(ctx)error)error; Metrics()->http.Handler. Options tracer là trace.TracerProvider, PoolStats callback trả3int64(total,acquired,max).

- [x] RED tests chưa có observer, rồi GREEN: real mux2resourceIDs→1label, status/no-content/1xx/duplicate headers, unknownmethod/route, sanitized logs/attributes và parent context, worker error/panic metric cleanup, concurrent scrapes. Early gateway errors kiểm ở Task2 khi nối app.
- [x] Implement bằng official clients, no custom Prometheus encoder; SDK provider do composition root cấp ở Task2. Observer standalone thiếu provider dùng noop rõ ràng.
- [x] Unit/race và gate package mới≥85%; exporter dùng in-memory trong unit chỉ là adapter fixture, không gọi là collector smoke.

### Task2: runtime, private ops and queue propagation

Files: internal/platform/{db.go,queue.go,*integration_test.go}; internal/app/{http.go,system.go,ops.go,config.go,*test.go}; cmd/server/main.go; internal/observability/tracing.go; compose.yaml/.env.example/README; docs/backend/runbooks/observability.md.

Interfaces: DB.Observer *observability.Observer; Event TraceParent/TraceState optional strings; app.Config.Observer; NewOpsHandler(db,limiter,observer)http.Handler. RuntimeConfig OpsListenAddr/TraceEndpoint; OTLP endpoint đầy đủ URL, no userinfo/query/fragment, http/https; blank không export. NewTraceProvider(ctx,endpoint) returns provider with bounded batch queue+timeouts, shutdown caller bounded5s.

- [ ] RED app/config tests ops default127.0.0.1:8081, malformed endpoint/listener bị từ chối không lộ input; ops metrics có, API metrics404, ops không có business route; readiness503 nếu DB unavailable.
- [ ] RED real PostgreSQL encrypted queue API-span→persisted context→2attempt worker spans cùngtrace, một lần lỗi rồi retry, no payload inlogs; job không context vẫn hoàn tất.
- [ ] Implement runtime server vàops start/stop chung context; fail listener nào cũng shutdown; worker-only vẫn cóops, appObserver được set trướcOpenQueue.
- [ ] Real local OTLP protobuf receiver test nhận spans qua HTTP và boundedshutdown; không gọi đây là Tempo/production smoke.
- [ ] Verify full unit/integration/race/build/openapi/security/secrets, runtime Docker smoke latest; report, commit, push feature và merge main.

Self-review: public healthz/readyz hiện dùng bởi clients/smoke được giữ tương thích ở checkpoint này; metrics chỉ ops. Không tự tick yêu cầu private toàn bộ health/API proxy từ compose development. Logs không ghi rawerror. Chưa làm filesystem/nodeexporter, SLO alerts hoặc deploy telemetry backend production.

Task1 evidence: missing Observer APIs RED→GREEN, rồi flush test RED vì unwrap bỏ qua status200 thực tế→FlushError ghi status trước flush→GREEN. `make verify integration security`:495Go/0fail/0skip,7Python tests; global85.95%, observability95.65%; No vulnerabilities found. Task1 chỉ cung cấp observer, runtime chưa nối tới Task2.
