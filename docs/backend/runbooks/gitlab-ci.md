# Xử lý pipeline GitLab bị kẹt trước khi chạy

Job chưa được runner nhận không có test trace. Phân biệt với lỗi script trước khi sửa code hoặc tăng timeout.

## Bằng chứng ngày13/11/2026

Project96, pipeline206 trên `baocao` tại651da36; job949 `backend-verify` có `runner=null`, `started_at=null`, `queued_duration=6051.254844`, `failure_reason=stuck_or_timeout_failure`. Job950 integration bị skip. UI xác nhận “This job does not have a trace”. Pipeline197 cũng chưa từng được runner nhận.

Tài khoản đang đăng nhập `phucnn220041` có vai trò Developer (đã kiểm trang Members). Settings CI/CD và danh sách runner trả404 trong phiên này; chưa có quyền gán/đăng ký runner. Không kết luận runner nào đang offline hoặc tag nào đúng khi chưa xem được cấu hình.

## Thao tác cần Owner/Maintainer

1. Mở project → Settings → CI/CD → Runners. Kiểm tra runner đã được gán cho project, không paused và có manager online.
2. Nếu dùng runner có sẵn, bật cho project. Backend jobs hiện không có `tags`; runner phải nhận **Run untagged jobs**. Nếu hạ tầng bắt buộc tag, cung cấp đúng tag đã được quản trị cấu hình rồi cập nhật YAML theo tag đó; không đoán tag.
3. Kiểm tra runner được phép chạy cả `baocao` và `baocao-phucnn`, kể cả trạng thái protected của hai nhánh. Dùng runner theo chính sách project; không bỏ bảo vệ runner dùng chung để chữa tạm.
4. Executor cần **Docker/Linux** để áp dụng `image` và bốn `services` trong `.gitlab-ci.yml`; shell executor không cung cấp các service container này. Job không yêu cầu privileged mode hoặc Docker socket bên trong job.
5. Nếu chưa có runner, Owner tạo project runner riêng trên máy CI chuyên dụng, đăng ký theo hướng dẫn GitLab. Giữ authentication token ngoài Git/log; không gửi token vào chat hoặc cấp quyền cho mọi project.
6. Retry job mới nhất. Chỉ coi scheduling đã sửa khi job có runner, started_at và trace. Sau đó phải chờ cả verify và integration thật kết thúc; nếu script fail, đọc lỗi trong trace của chính SHA đó.

Không bỏ job/test, đổi `allow_failure`, chuyển thành manual hoặc tăng timeout để che lỗi thiếu runner. Các phép kiểm local không thay kết quả remote.

## Đọc trạng thái công khai, không cần token

Từ repository root:

```sh
curl --fail --silent --show-error \
  'https://gitlab.nqtuan0192.me/api/v4/projects/96/pipelines?per_page=5'
curl --fail --silent --show-error \
  'https://gitlab.nqtuan0192.me/api/v4/projects/96/pipelines/206/jobs'
```

Cloudflare chặn User-Agent của Python urllib bằng1010 trong lần kiểm này; curl tiêu chuẩn nhận200. Không tắt TLS verification. Endpoint job detail/trace và runner cần quyền đăng nhập; không coi401 của chúng là lỗi code backend.

Tài liệu chính thức: [runner scope và quyền quản lý](https://docs.gitlab.com/ci/runners/runners_scope/), [runner tags/untagged/protected](https://docs.gitlab.com/ci/runners/configure_runners/), [Docker executor](https://docs.gitlab.com/runner/executors/docker/).
