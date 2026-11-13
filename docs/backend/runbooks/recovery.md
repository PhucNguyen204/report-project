# Kiểm tra backup và khôi phục PostgreSQL16

Quy trình dành cho fixture tổng hợp local `dicho-qa-postgres-1/dicho_test`, vai trò `dicho`. API/worker `dicho-qa-app-1` tiếp tục chạy trong suốt phép kiểm. Không dùng kết quả local để xác nhận RPO≤5phút, RTO≤1giờ hoặc chính sách giữ7bản backup của production.

## Điều kiện

- Docker đang chạy; container PostgreSQL16 và ứng dụng QA đang hoạt động. Vai trò local có quyền đọc dữ liệu và tạo/xóa database tạm. `pg_dump`, `pg_restore`, `psql`, `createdb`, `dropdb` được chạy trong container PostgreSQL cùng phiên bản.
- Python3 và Go theo `backend/go.mod` có sẵn trên host. Worker trial cần cổng PostgreSQL publish trên loopback và cấu hình `DATABASE_URL` hiện có trong container ứng dụng.
- Fixture phải có nhóm, danh sách/mặt hàng, lô và movement, công thức, lịch và tệp xuất. Có thể chuẩn bị fixture bằng network smoke hiện có; không chạy smoke nhắm tới dữ liệu thật.
- Đủ dung lượng cho archive và database tạm. Chỉ chạy với nguồn tin cậy: restore thực thi các định nghĩa SQL trong archive.

## Chạy tự động

Từ thư mục gốc repository:

```bash
python3 backend/scripts/recovery_check.py
```

Script ghi bằng chứng tại `output/backend-qa/recovery.json` và giữ archive `.dump` có tên sinh mới bên cạnh, quyền `0600`. Không đưa các artifact này vào Git. JSON chứa phiên bản, lệnh không có credential, thời gian, checksum, counts/SHA256, tổng tiền/lượng, integrity, kết quả worker retry và trạng thái dọn database tạm. Exit0 chỉ khi các bước được chọn và cleanup đều đạt.

Khi chỉ cần phép kiểm archive/restore và không có Go/cổng publish cho worker trial:

```bash
python3 backend/scripts/recovery_check.py --skip-worker-retry
```

Nhánh này ghi worker `NOT_RUN`; không dùng nó để xác nhận retry. Các tham số `--postgres-container`, `--app-container`, `--source-db`, `--user`, `--report` dùng để chọn đúng fixture và nơi lưu artifact. Script không nhận tên database đích do người gọi cung cấp; đích luôn có tên sinh mới `dicho_restore_<UTC>_<random>`.

## Cơ chế nhất quán và kiểm chứng

1. Giữ transaction `REPEATABLE READ READ ONLY`, gọi `pg_export_snapshot()`. Cả fingerprint nguồn và `pg_dump --snapshot` đọc cùng snapshot; ứng dụng tiếp tục ghi bình thường. Nếu DDL giữ khóa quá5giây, dump thất bại thay vì chờ vô hạn. Transaction xuất snapshot được đóng ngay sau dump.
2. Dùng custom archive toàn database, `--no-owner --no-acl`. Tạo database từ `template0`, chỉ sau khi xác nhận tên chưa tồn tại. Restore bằng `--exit-on-error --single-transaction`, không dùng `--clean` hoặc `--create`.
3. So sánh counts và SHA256 của toàn bộ dòng canonical JSON theo thứ tự hash cho tất cả bảng ứng dụng, cùng view báo cáo. So sánh riêng tiền VND và lượng theo đại lượng bằng PostgreSQL numeric/text, tránh float. Kiểm tra FK đã validate, liên kết cùng không gian, số dư lô/movement, công thức/lịch và tham chiếu ảnh.
4. Ghi nhận những quan hệ nguồn thay đổi sau snapshot. Đây là thông tin hoạt động của nguồn, không thay thế baseline snapshot và không làm sai phép so sánh restore.
5. Worker trial tạo fixture mới chỉ trong database đã khôi phục. River chạy queue cô lập `recovery`, gọi production `Dispatcher` từ checkout hiện tại cho `space.copy`; sau lần đầu, `JobRetry` phát lại cùng job. Counts, lượng, tiền và fingerprint dữ liệu đích phải giữ nguyên, với đúng1 worker receipt. Queue này không chạy SMTP, S3 hoặc scheduler. JSON ghi revision checkout và checksum script; worker trial không chạy binary của image QA đang hoạt động.
6. `finally` chỉ xóa database đã được chính lần chạy tạo thành công; tên phải khớp prefix và khác nguồn. Archive được giữ lại. Không stop/pause/restart source, không xóa volume hoặc database có sẵn.

Snapshot synchronization và custom archive theo tài liệu chính thức: [pg_dump PostgreSQL16](https://www.postgresql.org/docs/16/app-pgdump.html), [snapshot synchronization](https://www.postgresql.org/docs/16/functions-admin.html#FUNCTIONS-SNAPSHOT-SYNCHRONIZATION).

## Khôi phục archive thủ công vào đích mới

Đọc đường dẫn archive và SHA256 trong JSON, rồi đối chiếu checksum trước khi restore:

```bash
ARCHIVE="$(python3 -c 'import json; print(json.load(open("output/backend-qa/recovery.json"))["archive"]["path"])')"
shasum -a 256 "$ARCHIVE"
chmod 600 "$ARCHIVE"
RECOVERY_DB="dicho_restore_manual_$(date -u +%Y%m%d%H%M%S)_$RANDOM"
RECOVERY_CREATED=0
if docker exec dicho-qa-postgres-1 createdb -U dicho --template=template0 "$RECOVERY_DB"; then
  RECOVERY_CREATED=1
  docker exec -i dicho-qa-postgres-1 pg_restore -U dicho -d "$RECOVERY_DB" \
    --no-owner --no-acl --exit-on-error --single-transaction < "$ARCHIVE"
fi
```

`createdb` phải thành công với tên mới trước khi chạy restore; nếu tên đã tồn tại, dừng và chọn tên khác. Không thêm `--clean`. Chỉ trỏ ứng dụng vào đích khi kiểm chứng dữ liệu, phân quyền, migration/version và cấu hình secret đã hoàn tất trong một quy trình riêng.

Sau kiểm tra, chỉ dọn đúng database tạm vừa tạo:

```bash
case "$RECOVERY_CREATED:$RECOVERY_DB" in
  1:dicho_restore_manual_*) docker exec dicho-qa-postgres-1 dropdb -U dicho --force "$RECOVERY_DB" ;;
  *) echo 'Refusing unsafe database name' >&2; exit 1 ;;
esac
```

## Phạm vi và giới hạn

- Đây là phép kiểm snapshot logic của fixture nhỏ, cùng host/container PostgreSQL. Không chứng minh phục hồi sau mất host, PITR/WAL, backup offsite, RPO/RTO production hoặc việc giữ và luân chuyển7bản.
- Archive chỉ chứa database. S3 chứa ảnh/tệp xuất, Redis, hộp thư SMTP, role/tablespace toàn cluster, cấu hình hạ tầng và key trên filesystem cần quy trình backup/restore riêng.
- Không đọc hoặc chép signing/queue key vào archive/Git. Worker trial dùng sự kiện tổng hợp mới trên queue riêng, không cần các key hiện có; chưa chứng minh phát lại job mã hóa đã có trong archive. Khôi phục job mã hóa production cần cùng queue key được quản lý riêng.
- Connection URL cho worker trial được đọc vào bộ nhớ từ container ứng dụng rồi chỉ truyền qua environment của tiến trình Go; không ghi credential vào lệnh hoặc JSON. Lỗi nhà cung cấp được lược bỏ khỏi artifact.
- Fingerprint dùng aggregate trong bộ nhớ và phù hợp fixture local. Database lớn cần benchmark, chia batch/stream hash, kế hoạch dung lượng và diễn tập riêng. Việc kiểm tra sẽ giữ MVCC snapshot trong thời gian dump; cần theo dõi vacuum/bloat cho workload thực.
- Các kết quả đo chỉ nằm trong artifact của lần chạy; runbook không tự đánh dấu các gate nghiệm thu production là đã đạt.
