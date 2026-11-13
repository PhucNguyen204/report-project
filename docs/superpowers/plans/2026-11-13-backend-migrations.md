# Backend migration privileges plan

> Root tự review và triển khai theo quyền đã cấp. TDD + verification-before-completion; không sửa migration đã commit.

**Goal:** production API/worker chạy bằng role không có quyền DDL; migrator riêng chỉ cần DATABASE_URL. Development vẫn khởi tạo tự động rõ cấu hình.

**Architecture:** tách MigrateQueue/OpenQueue; ConfigureQueue giữ wrapper bootstrap cho caller hiện có. NewSystem chỉ OpenQueue kiểm queue schema, không chạy DDL. Main dùng AUTO_MIGRATE mặc định true ở development, false ở production; trước startup kiểm app migration checksums và River validation. Binary dicho-migrate chạy cả hai migration với role riêng rồi thoát.

**Tech:** Go1.26.11, PostgreSQL16, River version từ go.mod. Source: chapters/c3/bao-mat.tex (role không đổi lược đồ); runtime plan và F37 gate.

## Task1: Read-only schema checks and queue startup

Files: platform/{migrate.go,queue.go,migration_privileges_integration_test.go}; app/system.go.

Interfaces: CheckMigrations(ctx,pool,fs.FS)error; MigrateQueue(ctx,pool)error; OpenQueue(ctx,db,handler,keys...) returns same River client as ConfigureQueue. ConfigureQueue=MigrateQueue+OpenQueue. OpenQueue dùng rivermigrate.Validate, không migration DDL.

- [x] RED CheckMigrations: fresh current ledger pass, missing row/changed checksum/unknown ahead migration fail, context cancellation propagates; no create/update statements.
- [x] RED OpenQueue bằng role LOGIN không sở hữu DB, được DML/USAGE/sequence với schema hiện có nhưng không CREATE: queue configure/insert/start/job completion đạt, CREATE TABLE bị denied. NewSystem dùng OpenQueue.
- [x] GREEN minimal, giữ encryption/transaction enqueue semantics và tests cũ.

## Task2: Separate migrator and explicit runtime policy

Files: cmd/migrate/main.go, cmd/server/main.go, app/{config.go,config_test.go}, Dockerfile/Makefile/.env.example/README và runbook privileges.

- [x] RED runtime config AUTO_MIGRATE defaults development true/production false, explicit valid bool honored, invalid fails; production rejects true to enforce separate role.
- [x] Migrator chỉ đọc DATABASE_URL, bounded context, chạy app+River migrations và generic errors không lộ DSN; build dedicated binary cùng image. Main auto/dev or check/prod, System no DDL.
- [x] Real fixture limited-role API registration + DB readback/ready check plus worker completion; doc grants/schema_migrations readonly, no owner/CREATE; fresh development bootstrap still passes.
- [x] Full verify/integration/race/security, report/commit/push/merge F01-migration-privileges.

Review focus: ledger database ahead phải báo lỗi thay vì chạy binary cũ; River schema cần validate trước client; migration-only CLI không yêu cầu FCM/S3/signing key; test role cleanup không thay role/dữ liệu có sẵn; không che lỗi migration bằng fallback role cao.

Self-review: preserve ConfigureQueue public bootstrap compatibility, production denies AUTO_MIGRATE=true; riêng DDL role không được nhúng vào runtime config/image. Không tự deploy hoặc đổi quyền database user có sẵn của người dùng; diễn tập trên generated test DB/role.

Verification13/11: staged snapshot399 pass/0 fail/0 skip; bổ sung APIregistration/ready bằng role giới hạn và targetedracePASS, full working tree407 pass/0 fail/0 skip (bao gồm search đang chốt riêng). Binary Docker UID10001 chạy migrator2lần trên DB trống riêng exit0; DB dọn sau thử. Govulncheck no vulnerabilities; staged secret scan exit0. Test bootstrap đầu sai fixture do ConnString giữ DSN cũ, đã sửa URL đích rõ ràng rồi GREEN. Production privileges chưa thay đổi.
