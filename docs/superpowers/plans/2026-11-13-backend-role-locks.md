# Giữ quyền actor và thống nhất thứ tự khóa ảnh/template

**Goal:** mutation đang được authorize giữ role hiện tại đến commit; việc thu hồi role không đi qua cửa sổ chờ resource lock. Không thêm I/O nhà cung cấp vào DB transaction.

**Scope:** follow-up review F34-admin-revocation, planning/templates.go và media/service.go. Root dùng TDD, full regression, report rồi commit/push/merge; không hỏi lựa chọn thường lệ.

## Task1: Template và media actor authority

Files: `planning/{templates.go,admin_authority_integration_test.go}`, `media/{service.go,authority_integration_test.go}`, test helper `testutil/lock.go` nếu cần.

- [x] RED template SaveTemplate chờ recipe row; demotion không được hoàn tất trước transaction đã authorize. Media Upload template tương tự, expected demotion chờ actor lock; sau commit role mới có hiệu lực với upload/complete tiếp theo.
- [x] requireAdmin đọc actor FOR SHARE, no rows→Forbidden. actorRole giữ FOR SHARE. Không thay last-admin serialization ở accounts.
- [x] GREEN hai test trên với PostgreSQL barriers, không dùng sleep để giả lập thứ tự.

## Task2: Complete lock ordering

- [x] Sau thêm actor lock, RED owned/Complete đang đợi profile user row không được giữ object FOR UPDATE trước; profile holder phải lấy object FOR SHARE NOWAIT được.
- [x] owned lấy actor, đọc purpose/target bất biến không khóa object, authorize/khóa target, rồi đọc lại trạng thái/expiry và khóa object FOR UPDATE. Giữ các kiểm tra role/ownership/purpose/target của Validate; không tái dùng quyết định quyền từ transaction trước.
- [x] GREEN concurrency và toàn media/planning suites; S3 vẫn ngoài transaction, upload/complete/read không dùng quyền cũ sau thay đổi.
- [x] Root full verify/integration/race/security, secret diff scan; report/commit/push/merge F09/F33-authority-locks.

Self-review: giữ actor→target→object trong upload/complete; caller profile/recipe có thể đã giữ target row nhưng actor share tương thích và object chưa khóa khi chờ actor. Read chỉ share locks; không đổi TTL signed URL15phút hoặc hứa thu hồi URL đã ký. Không khóa household.spaces sớm gây đảo thứ tự Activity.

Verification13/11: hai authority tests RED demotion hoàn tất trước khi chờ khóa→GREEN. Sau thêm actor share, lock-order regression RED55P03 với object-before-actor; giữ actor→target→object và GREEN trên Complete replay ảnh ready/profile validation thật. Latest-ready concurrency test dùng object S3 thật và không thay immutable key. Full root make verify integration411 pass/0fail/0skip; media86.8%, planning87.7%.
