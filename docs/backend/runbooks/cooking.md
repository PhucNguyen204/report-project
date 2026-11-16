# Xác nhận đã nấu và kết quả trừ kho

`POST /it4788/v1/spaces/{sid}/meals/{mealId}/cook` cần JWT, `X-App-Version`, `Idempotency-Key` và `If-Match` đúng cú pháp. Người gọi phải còn là thành viên hiện tại của không gian. Ngày nấu không được ở tương lai; món cần gắn công thức.

`servings` chọn1–20 khẩu phần; bỏ qua để dùng khẩu phần đã lên lịch. `skipStock=true` xác nhận đã nấu và không trừ kho. Khi gửi `lines`, mỗi dòng có UUID thực phẩm/đơn vị, lượng không âm với tối đa2 chữ số thập phân; thực phẩm phải thuộc công thức và đơn vị cùng nhóm đại lượng đã lưu trong nguyên liệu.

- Lượng0: nguyên liệu lấy ngoài tủ, không ghi biến động. Dòng này không được kèm lựa chọn lô khác rỗng.
- Lượng dương và không chỉ định lô: FEFO trên các lô chưa hết hạn, cùng thực phẩm/không gian/nhóm đại lượng.
- `lotIds`: tối đa100UUID không trùng, chỉ dùng các lô đã chọn. Đây là lựa chọn tường minh, bao gồm lô quá hạn đã được người dùng xác nhận tại giao diện. Không dùng cùng `lotId` khác rỗng.
- `lotId` và `allowExpired` cũ vẫn được hỗ trợ: khi chọn một lô quá hạn, cần `allowExpired=true`.
- Bỏ hẳn `lines` vẫn tự tính FEFO để tương thích client cũ; client có màn xác nhận nên gửi lượng đã duyệt cho từng nguyên liệu. Không gửi quantity0 vào API dùng thực phẩm P05; P05 vẫn yêu cầu lượng dương.

Phản hồi giữ các trường món ở dạng phẳng và bổ sung `meal`, `deductions`, `alreadyCooked`. `meal.cookedAt`/`cookedBy` lấy từ dữ liệu đã lưu. Actor cũ không đủ bằng chứng migration có thể lànull; không dùng người gọi lại làm người đã nấu. `deductions` liệt kê foodId/lotId/quantity/unitId/dimension: lượng theo đơn vị cơ sởg/ml/cái, không phải đơn vị nhập của từng dòng. Các đại lượng không được cộng lẫn nhau.

Lần xác nhận mới trả `alreadyCooked=false`; gọi lại bằng key mới cho món đã nấu trảtrue, giữ nguyên actor/thời điểm/lịch sử và không trừ lại. Retry cùng idempotency key giữ đúng nội dung yêu cầu và trả lại phản hồi gốc với header `Idempotent-Replayed`; cờ alreadyCooked trong phản hồi cache vẫn là giá trị gốc. Nếu thay đổi lượng hoặc danh sách lô sau khi xem lại, dùng key mới.

Thiếu tồn hoặc lô hợp lệ đã dùng hết/loại bỏ sau bước xem trước trả409/4002 kèm `current`. Snapshot chỉ gồm lô còn dùng được trong đúng không gian/thực phẩm/nhóm đại lượng, với `baseQuantity` theo đơn vị cơ sở. Tất cả trừ kho của lượt lỗi được rollback trước khi đọc snapshot; món vẫn dự kiến. Lô không tồn tại, khác nhóm/thực phẩm/đại lượng hoặc đã xóa nhận404. Xung đột phiên bản món cũng trả409; khi không có current thì tải lại thông tin món. API P05 giữ lỗi thiếu lượng422/2003.

Lịch sử trả theo foodId/lotId bất biến; sửa hạn dùng không làm đổi thứ tự replay. Nếu quản trị làm mất đơn vị cơ sở đang cần để biểu diễn lịch sử, server trả503 thay vì báo danh sách trừ rỗng. Xác nhận mới bị lỗi lịch sử cũng rollback toàn bộ giao dịch. Metadata cooked không thay đổi cùng cấu hình mặc định của thực phẩm, và allocation không được dùng ml để đáp ứngg.

Xem hợp đồng `/openapi.json`, [ADR12 và plan](../../superpowers/plans/2026-11-15-backend-cooking-contract.md) và [báo cáo kiểm chứng](../PROGRESS.md). Tệp này mô tả hành vi hiện thực; kết quả từng lệnh, commit và các gate còn mở được ghi trong báo cáo chung.
