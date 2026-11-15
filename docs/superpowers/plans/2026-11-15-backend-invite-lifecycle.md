# UC06 invitation lifecycle implementation plan

> **For agentic workers:** TDD và verification-before-completion; agent giữ accounts/groups.go và test mới; root giữ OpenAPI, báo cáo và Git. Người dùng đã yêu cầu tự review rồi triển khai, dùng sub-agent, không hỏi lựa chọn thông thường.

**Goal:** hoàn thiện UC06 A2b/A3a/A3b/B6 và các quy tắc G07 còn thiếu, giữ tương thích các trường phản hồi hiện có.

**Architecture:** GET hiện trạng mã không tạo/rotate; POST vẫn là lệnh tạo mới/rotate rõ ràng. Quyền và sức chứa được kiểm trong family advisory transaction hiện có. Xác nhận tham gia giữ gateway/idempotency và transaction nghiệp vụ.

**Tech Stack:** Go/pgx/PostgreSQL hiện có, không thêm thư viện hoặc migration.

**Spec:** chapters/c2/pr-nhom.tex UC06, backmatter/pl/f-dac-ta-api.tex G05/G07, AGENTS.md, backend/api/build.py hiện hành.

## Constraints và ADR11

- Giữ25UC, không sửa LaTeX. Trường id/name/memberCount/dryRun hiện có giữ nguyên; bổ sung leaderName/maxMembers và role/joinedAt để client hiện tại không bị thay bằng nested space như một ví dụ ở phụ lục.
- GET /it4788/v1/spaces/{sid}/invites/current, dùng mã API G05 như operation bổ sung. Response status=none/active/expired, code/expiresAt/link nullable, memberCount/maxMembers. Không có mã hoặc đã revoke → none; mã quá hạn → expired, không tự tạo mới. Chỉ leader hiện tại, outsider/admin404 và member403.
- Theo UC06 A2b, GET và POST từ chối422 khi nhóm đầy, kiểm quyền trước sức chứa. DELETE revoke vẫn được dùng khi nhóm đầy. POST kiểm trước khi revoke mã cũ để lỗi không làm mất mã.
- Join code bỏ mọi Unicode whitespace, uppercase, đúng8 ký tự alphabet ABCDEFGHJKLMNPQRSTUVWXYZ23456789; malformed400 trước tra mã, malformed vẫn tính quota10 lần/giờ để không mở đường dò. Session/actor bị revoke không được ghi bộ đếm; dùng transaction độc lập có guard để giữ counter hợp lệ khi lệnh nghiệp vụ bị từ chối, không giữ lock qua family advisory.
- Preview trả leaderName/maxMembers, role rỗng/joinedAt null khi chưa tham gia. Confirm trả role/joinedAt của membership. Cùng code còn hiệu lực và đã thuộc đúng nhóm trả membership hiện có, kể cả nhóm đầy; không phát notification/activity lần nữa. Thuộc nhóm khác vẫn422. Quy tắc mã sai/hết hạn/revoked không thay.

## Review focus

1. Reload trang mời không rotate hoặc kéo dài TTL; hết hạn/revoke không tự hồi sinh mã.
2. Create ở nhóm đầy không revoke mã đang có; invite/join/capacity race vẫn dùng cùng advisory.
3. Transfer/remove/logout/disable trong lúc chờ khóa không lộ mã hoặc ghi counter trái quyền.
4. Already-member retry với key khác không tăng member/activity/notification; preview không join.
5. DTO add field đúng schema và không lộ danh tính leader cho người không có code hợp lệ.

### Task1: accounts service và tests

Files: backend/internal/modules/accounts/groups.go; new invite_lifecycle_integration_test.go (có thể thêm join_format_integration_test.go).

- [x] Viết test GET none/repeatedactive/expired/revoked, quyền và fullgroup; quan sát RED endpoint404/mutation được chấp nhận sai.
- [x] Implement currentInvite + register, count/max check trước create/revoke, response nullable đầy đủ; chạy GREEN.
- [x] Viết test normalize internal whitespace/forbiddenalphabet/length, preview leader/capacity, samegroup repeatedconfirm, stale-auth counter; quan sát RED cụ thể.
- [x] Implement join validation/guard/metadata và existingmembership branch; kiểm family advisory/actor lock order; chạy realPG/race, kiểm không tăng effect khi replay.
- [x] Chạy full default suite và accounts integration/staticcheck; báo lệnh/kết quả, root review.

### Task2: contract và checkpoint

Files: backend/api/build.py + openapi.json; app/openapi_integration_test.go; docs/backend/PROGRESS.md.

- [x] Contract exchange GET/POST/preview/confirm test RED trước schema; thêm InviteState và JoinResult fields với nullable joinedAt.
- [x] Full make verify integration security, secret scan; review diff, ghi hạn chế live provider/CI.
- [ ] Commit Feature-ID F13-invite-lifecycle; push feature GitLab/GitHub rồi merge main không force.

Self-review: phần chia sẻ/QR/camera là client; backend chỉ cung cấp mã và link. Không đổi idempotency POST thành GET tạo ngầm. Không thêm quyền admin trên nhóm.
