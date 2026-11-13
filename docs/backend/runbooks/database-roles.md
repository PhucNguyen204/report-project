# Tách migrator và role database của API/worker

Production API/worker không chạy DDL. `AUTO_MIGRATE` mặc định false trong production; cấu hình true bị từ chối. Development mặc định true và có thể tắt khi dùng cùng mô hình role giới hạn.

## Trình tự triển khai

1. Dùng role sở hữu schema trong một tiến trình migration riêng:

```sh
cd backend
DATABASE_URL="$MIGRATION_DATABASE_URL" go run ./cmd/migrate
```

Binary build là `bin/dicho-migrate`; image có `/usr/local/bin/dicho-migrate`. CLI chỉ cần DATABASE_URL, không cần khóa ký, S3, SMTP hoặc FCM. Giữ DSN trong secret store/environment được che, không ghi ra log/Git. Lệnh kiểm checksum và chạy app/River migrations; chạy lại không đổi dữ liệu đã áp dụng.

2. Sau migration, DBA cấp quyền đối tượng cho role runtime riêng. Ví dụ dưới dùng role `dicho_app` đã được DBA tạo với LOGIN, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, không sở hữu database/schema/table. Tạo mật khẩu qua cơ chế secret của hạ tầng, không đưa vào script.

```sql
GRANT CONNECT ON DATABASE dicho TO dicho_app;
GRANT USAGE ON SCHEMA public,platform,identity,household,catalog,admin,
  pantry,shopping,recipe,mealplan,report,media,notification TO dicho_app;
GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA
  public,platform,identity,household,catalog,admin,pantry,shopping,
  recipe,mealplan,report,media,notification TO dicho_app;
GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA
  public,platform,identity,household,catalog,admin,pantry,shopping,
  recipe,mealplan,report,media,notification TO dicho_app;
REVOKE INSERT,UPDATE,DELETE ON public.schema_migrations,public.river_migration FROM dicho_app;
REVOKE UPDATE,DELETE ON admin.audit_logs FROM dicho_app;
```

Đổi database/role theo hạ tầng. Kiểm tra `PUBLIC` và membership của role không gián tiếp cấp CREATE, ownership, BYPASSRLS hoặc quyền quản trị khác; các GRANT trên không tự thu hồi quyền cao đã có. Reapply grants sau khi migration thêm đối tượng; có thể dùng ALTER DEFAULT PRIVILEGES theo đúng schema-owner trong quy trình DBA. Không dùng tài khoản migrator làm DATABASE_URL của app.

3. Chạy API/worker bằng DSN runtime, `APP_ENV=production`, `AUTO_MIGRATE=false`. Startup đọc ledger và đối chiếu checksum bundle; missing/changed/unknown-ahead app migration hoặc River schema chưa phù hợp đều làm startup thất bại. Không tự nâng quyền hoặc tự sửa schema.

4. Kiểm chứng trong môi trường triển khai: tạo tài khoản/nhóm, ghi/đọc nghiệp vụ, enqueue/worker và audit; thử CREATE TABLE và sửa ledger phải bị denied. Test local đã chứng minh DDL bị chặn, ledger chỉ đọc và River encrypted job hoàn thành với role riêng; quyền production vẫn cần kiểm chứng tại nơi triển khai.

## Lưu ý vận hành

- NewSystem chỉ mở queue, không migrate. `platform.ConfigureQueue` còn là wrapper bootstrap cho fixture/caller cũ; runtime dùng OpenQueue.
- Migration chạy tuần tự trước khi bật phiên bản API/worker mới. Không chạy binary cũ với schema ahead; không sửa checksum migration đã commit.
- Giữ và backup queue key riêng để đọc job mã hóa cũ. Role separation không thay thế backup/PITR hoặc quy trình rollback dữ liệu.
