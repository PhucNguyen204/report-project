# Metrics, log và trace backend

Runtime luôn tạo observer riêng cho process. `OPS_LISTEN_ADDR` mặc định `127.0.0.1:8081`, có `GET /healthz`, `/readyz`, `/metrics`, cả trong MODE=worker. API không đăng ký `/metrics`. Compose bind ops vào container nhưng không publish port8081; Prometheus phải cùng mạng nội bộ. Public health/ready hiện còn để tương thích; reverse proxy production chỉ cho đi tới API nghiệp vụ, không route ops.

Các metric chính:

| Metric | Labels/nội dung |
|---|---|
| dicho_http_requests_total | method, route pattern, status |
| dicho_http_request_duration_seconds | histogram cùng labels; latency toàn gateway |
| dicho_http_requests_active | số request đang chạy |
| dicho_worker_attempts_total | kind thuộc whitelist, result=ok/error/panic; mỗi retry tính một attempt |
| dicho_worker_attempt_duration_seconds | thời gian attempt, gồm decrypt/handler |
| dicho_worker_attempts_active | số attempt đang chạy |
| dicho_db_connections / _acquired / _max | kích thước và mức dùng pgx pool |
| go_* / process_* | runtime/process do official collector hỗ trợ trên OS |

Ví dụ truy vấn Prometheus (cần scrape thật trước khi dùng):

```promql
sum(rate(dicho_http_requests_total[5m])) by (route)
sum(rate(dicho_http_requests_total{status=~"5.."}[5m])) by (route)
histogram_quantile(0.95, sum(rate(dicho_http_request_duration_seconds_bucket[5m])) by (le, route))
sum(rate(dicho_worker_attempts_total{result!="ok"}[5m])) by (kind)
dicho_db_connections_acquired / dicho_db_connections_max
```

HTTP log JSON mang request_id, trace_id, method, route, status, duration_ms; mã đối chiếu được trả trong X-Request-ID/X-Trace-ID. Lỗi JSON có thêm traceId khi observer hoạt động. Không dùng raw path/query/user/space/header/body làm labels hoặc log. Unknown methods/routes/events được gộp; client tracestate và baggage không được truyền vào span. Traceparent chuẩn W3C chỉ là correlation, không có giá trị phân quyền.

Queue lưu traceparent/tracestate optional bên cạnh payload AES; metadata này không phải bí mật, không tham gia unique key hoặc thay AAD cũ. Worker tạo span consumer mới cho mỗi attempt; job không context tạo trace mới. Job log giữ kind/attempt/result/trace_id; opaque error và panic không được ghi. River logger được lọc còn severity/component và message tổng quát, tránh lỗi provider mang dữ liệu người dùng. Lỗi chi tiết được xử lý trong hệ thống nghiệp vụ/DB theo quyền vận hành, không đưa vào log telemetry.

`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` bỏ trống: không gửi trace qua HTTP, kể cả SDK có biến môi trường endpoint khác. Khi triển khai collector, đặt URL đầy đủ có path, ví dụ `http://collector:4318/v1/traces` trên mạng nội bộ hoặc HTTPS đã xác thực certificate. Không chấp nhận userinfo/query/fragment; không nhận headers xác thực từ biến môi trường của exporter và không follow redirect. Collector có auth riêng cần thêm cấu hình credential chuyên biệt trước khi dùng, không nhét token vào URL.

Runtime dùng parent-based sampling với10% root, batch queue1024/128 spans một batch, timeout2s, không retry exporter; API/transaction không chờ network export. Queue đầy có thể bỏ span. Shutdown dành tối đa5s cho provider, lỗi export được ghi chung. Metrics requests/attempts vẫn đếm độc lập sampling; không dùng số span để suy ra đủ số request hoặc reminder.

Giới hạn nghiệm thu: local OTLP receiver chứng minh protobuf/HTTP, không chứng minh Tempo/Alloy/Loki production. Chưa có filesystem/node-exporter, queue waiting-age/backlog collector, SLO alert deployment hoặc monitoring đủ kỳ. Chưa thể xác nhận reminder99%/15phút, availability99%, RPO/RTO production chỉ bằng các metric này.

Tài liệu đối chiếu: [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/instrumentation/), [OTLP exporters](https://opentelemetry.io/docs/languages/go/exporters/), [Prometheus Go HTTP handler](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp).
