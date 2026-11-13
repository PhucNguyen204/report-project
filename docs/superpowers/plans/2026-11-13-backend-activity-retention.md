# Activity retention implementation plan

> **For agentic workers:** dùng TDD/verification-before-completion, root giữ Git; không đổi migration đã commit.

**Goal:** scheduler xóa platform.activities quá90ngày đúng bảng lưu giữ, giữ sequence và lịch sử nghiệp vụ/báo cáo.

**Architecture:** bổ sung DELETE có cutoff rõ ràng vào transaction Tick hiện có; scheduler slot chỉ commit khi mọi cleanup thành công. Migration048 mới thêm index created_at. Không hard-delete lots/movements/recipes/shopping hoặc audit_logs, vì báo cáo12tháng và nhật ký quản trị có thời hạn khác. Delta sync/tombstone cleanup tiếp tục ngoài scope theo ADR01.

**Tech Stack:** Go1.26.11, PostgreSQL16, pgx transaction và clock.

**Spec:** chapters/c3/csdl.tex bảngtab:c3-luu-giu; AGENTS.md định hướng offline; backend plan F29/F37.

## Global constraints

- Cutoff strict created_at<now−90×24h UTC; ở đúng biên giữ lại.
- Không reset change_seq hoặc xóa admin audit/business source rows.
- Lỗi cleanup rollback toàn Tick và scheduler slot để retry; không đánh SLA production từ test.

## Review focus

1. Dòng đúng90ngày và future vẫn còn, không xóa theo ngày địa phương lệch7giờ.
2. Nhiều spaces/actors được dọn đúng, sequence không bị tái sử dụng sau cleanup.
3. Audit logs quá90ngày vẫn giữ; stock movement/history không bị lẫn retention.
4. Lỗi DELETE sau khi notification cleanup chạy phải rollback cả notification và slot.
5. Retry sau bỏ lỗi thực hiện cleanup đúng một slot, không làm gãy tham chiếu.

### Task1: activity cleanup

Files: internal/app/system.go, activity_retention_integration_test.go; migrations/048_activity_retention.sql; PROGRESS.md.

- [x] RED fixture old/boundary/fresh/future trên2spaces chưa bị xóa; audit cũ giữ, seqcounter giữ và insert tiếp nhận seq mới.
- [x] RED PostgreSQL DELETE trigger lỗi sau notification cleanup: Tick lỗi/rollback, old notification/log vẫn còn và slot0; bỏ trigger→retry sạch/slot1.
- [x] Minimal cleanup +index migration mới; full verify/integration/security và review trước report; Git ngay sau staged gate.

Evidence:2RED thực tế (seq1 vẫn còn/trigger không được gọi)→GREEN; `make verify integration security`:522Go/0fail/0skip,7Python/global85.92%, app89.19; lint2buildsets/race/vet/build/mod verify PASS, No vulnerabilities found. Migration048 mới, không thay ledger cũ. Không claim tombstone/offline cleanup hoặc hard-delete business data; admin audit180ngày vẫn giữ.
