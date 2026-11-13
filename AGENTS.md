# Ghi chú phiên làm việc 2026-10-30 15:37

Tệp này ghi lại các quyết định phạm vi và thay đổi đã thực hiện, để các phiên làm việc sau tiếp tục nhất quán. Đọc phần "Thay đổi đã thực hiện so với phiên trước" trước khi sửa chương 1, 2, 3.

- Nguời thực hiện: Trần Gia Bảo
- Thời gian thực hiện: 2026-10-30 15:37
- Agent: Claude Code
- Commit: ...

## Cách làm việc

- Biên dịch báo cáo: `./run.sh --tectonic` (đã được phép chạy tectonic). Slide: `bash build-slides.sh`. Chưa có `latexmk`, nên không dùng `./run.sh` không tham số.
- Sơ đồ: sửa `diagrams/*.puml`, rồi `plantuml -tpng -o ../figures/diagrams <tệp>.puml` chạy trong `diagrams/`. Ảnh PNG được commit trong `figures/diagrams/`.
- `sed -i` trên macOS lỗi với đường dẫn dài; dùng Python để sửa hàng loạt.
- Ngôn ngữ báo cáo: tiếng Việt. Dùng macro `\uc{UCxx}`, `\br{BRxx}`, `\msg{MSGxx}`, `\nfr{NFRxx}` để tham chiếu.

## Thay đổi đã thực hiện/quyết định

1. **Chức năng ưu tiên phát triển** (chức năng mở rộng): NV01.4, NV01.6, NV02.6, NV02.7, NV03.1, NV04.3, NV05.3, NV05.4, NV07.3, NV07.4. Phần còn lại phải làm nhưng ở mức tối giản, không đặc tả luồng.
2. **Phân tầng use case:** tầng 1 (ưu tiên, đặc tả và kiểm thử đầy đủ), tầng 2 (nền tảng, chỉ luồng chính), tầng 3 (tối giản, yêu cầu đề bài gốc, không đặc tả luồng).
3. **Bỏ khỏi tiêu chí chấp nhận:** hoạt động ngoại tuyến và đồng bộ thời gian thực (MR10, NFR10, NFR11) chuyển thành định hướng phát triển.
4. **Bỏ hẳn quét mã vạch (MR04) và Open Food Facts.** Thực phẩm luôn nhập tay.
5. **Rời nhóm (CN15)** được nâng lên tầng 2, chỉ làm hai trường hợp: thành viên thường rời; trưởng nhóm duy nhất thì giải tán nhóm. Trưởng nhóm còn thành viên khác phải chuyển quyền trước (MSG24).
6. **Phân công người mua vẫn gửi thông báo đẩy** (không cắt). Quyết định này từng bị ghi nhầm là cắt; đã khôi phục.
7. **Quản trị viên là tác nhân độc lập, không kế thừa Người dùng** (tránh vi phạm BR23). Tác nhân phụ còn ba: Bộ lập lịch, Dịch vụ thông báo đẩy, Dịch vụ thư điện tử.
8. Phương án tái cấu trúc đã chọn: **gộp 53 chức năng thành 25 use case** theo mục tiêu người dùng và bỏ hẳn mã cũ (phương án B). Không tạo lại lớp mã trung gian.

## Mô hình use case hiện tại: 25 use case (UC01 đến UC25)

| Mã | Use case | Tầng | Gói |
|---|---|---|---|
| UC01 | Đăng ký và xác thực tài khoản | 2 | Tài khoản |
| UC02 | Đăng nhập và đăng xuất | 2 | Tài khoản |
| UC03 | Đặt lại và đổi mật khẩu | 3 | Tài khoản |
| UC04 | Quản lý hồ sơ và thiết lập nhắc nhở (*) | 1 | Tài khoản |
| UC05 | Tạo nhóm và xem thành viên | 2 | Nhóm |
| UC06 | Mời và tham gia nhóm bằng mã mời (*) | 1 | Nhóm |
| UC07 | Rời nhóm, xóa thành viên và chuyển quyền | 2 | Nhóm |
| UC08 | Quản lý danh sách và mặt hàng mua sắm (gồm sao chép, phân công, lịch sử) | 2 | Mua sắm |
| UC09 | Đánh dấu mặt hàng đã mua | 2 | Mua sắm |
| UC10 | Bổ sung nguyên liệu còn thiếu vào danh sách mua sắm (từ lịch hoặc từ công thức) (*) | 1 | Mua sắm |
| UC11 | Nhập thực phẩm vào tủ lạnh (từ đơn mua hoặc nhập tay) (*) | 1 | Tủ lạnh |
| UC12 | Xem và lọc thực phẩm trong tủ lạnh | 2 | Tủ lạnh |
| UC13 | Điều chỉnh lô thực phẩm (sửa, sử dụng, loại bỏ) | 2 | Tủ lạnh |
| UC14 | Nhắc thực phẩm sắp hết hạn | 2 | Tủ lạnh |
| UC15 | Quản lý công thức nấu ăn | 2 | Công thức |
| UC16 | Xem công thức và đối chiếu nguyên liệu | 2 | Công thức |
| UC17 | Quản lý lịch bữa ăn | 2 | Kế hoạch |
| UC18 | Gợi ý món ăn từ thực phẩm sẵn có (*) | 1 | Kế hoạch |
| UC19 | Xác nhận đã nấu món ăn (*) | 1 | Kế hoạch |
| UC20 | Tìm kiếm và lọc thực phẩm, món ăn | 3 | Tìm kiếm |
| UC21 | Xem báo cáo mua sắm, tiêu thụ và lãng phí (*) | 1 | Thống kê |
| UC22 | Xuất báo cáo (*) | 1 | Thống kê |
| UC23 | Quản lý danh mục dùng chung | 3 | Quản trị |
| UC24 | Quản lý tài khoản người dùng | 3 | Quản trị |
| UC25 | Cấu hình tham số hệ thống | 3 | Quản trị |

Tầng 1: 8 use case. Tầng 2: 12. Tầng 3: 5.

Quan hệ `extend` còn lại: UC11 mở rộng UC09; UC18 mở rộng UC17; UC22 mở rộng UC21. Không còn `include`. Thao tác gọi từ màn hình danh sách là điều hướng, không mô hình hóa bằng `extend`.

Năm kịch bản giá trị (bảng trong `chapters/c2/uc-tong-quan.tex`): K1 gia đình cùng tham gia; K2 đi chợ về cất vào tủ; K3 lập kế hoạch và bù phần thiếu; K4 nấu từ đồ sẵn có; K5 nhìn lại lãng phí.

## Quy ước đặc tả khi một use case gộp nhiều thao tác

- Mỗi use case có **một** bảng `ucspec` (nhãn `tab:UCxx`).
- Thao tác thành phần thành **biến thể A, B, C** trong cùng bảng. Bước của biến thể A giữ nguyên số; bước của biến thể B trở đi có tiền tố chữ (`B3a`, `C2a`). Tham chiếu bước ở chương 3 và phụ lục theo quy ước này.
- Tiêu đề mỗi use case trong gói dùng `\ucgoal{UCxx}{Tên}{tầng}{Tác nhân}{Gộp từ ...}` (định nghĩa trong `config/ucspec.tex`).
- Đặc tả use case tầng 2 và 3 nằm ở Phụ lục B (`backmatter/pl/b-dac-ta-bo-sung.tex`); tầng 1 ở các file `chapters/c2/pr-*.tex`. Đặc tả tầng 3 được giữ để tham khảo, không phải tiêu chí chấp nhận.
- Nhóm nghiệp vụ NV và các chức năng thành phần ban đầu (CN01 đến CN53) **không còn tồn tại** trong tài liệu. Chương 1 vẫn dùng mã NV và MR.

## Các tệp đã thay đổi

- Chương 1: `chuong1.tex` (MR10 và MR04 ghi rõ phạm vi; bảng MR theo mã UC mới; bỏ cơ sở dữ liệu sản phẩm mở khỏi ngữ cảnh).
- Chương 2: `chuong2.tex`, `c2/gioi-thieu.tex` (bảng 25 use case, bảng quan hệ, tác nhân), `c2/uc-tong-quan.tex` (bảng kịch bản), `c2/pr-*.tex` (đặc tả gộp), `c2/phi-chuc-nang.tex` (NFR10, NFR11 là định hướng).
- Chương 3: `c3/*.tex` đã đổi mã UC; đã gỡ toàn bộ thiết kế mã vạch (API P08, mã lỗi 5004, MSG23, `BarcodeService`, khóa Redis, cột `barcode`).
- Phụ lục: B (đặc tả bổ sung), D (truy vết), E (ma trận hiện thực, 25 dòng), C, F, G, H, tài liệu tham khảo.
- Cấu hình: `config/ucspec.tex` (macro `\ucgoal`, tiêu đề luồng tự xuống dòng).
- Sơ đồ: viết lại c2-02, 03, 05, 07, 10, 13, 14, 17, 18, 19; sửa c1-03, 04, 10, c2-01, c3-01, 02, 05b, 23, 26, 32, 39; xóa c3-19.
- Slide, `THUYET_TRINH_10_PHUT.md`, `README.md` đã đồng bộ số lượng và mã UC.

## Những vấn đề khác cần xem xét thêm

- **Ngoại tuyến và thời gian thực** vẫn được thiết kế chi tiết ở chương 3 (`ky-thuat.tex`, `tich-hop-di-dong.tex`, API `/changes`, WebSocket, header `X-Client-Time`). Các mục này chỉ được đánh dấu "Phạm vi: định hướng phát triển". Nếu cần gỡ hẳn thì phải thiết kế lại kiến trúc đồng bộ.
- Mã MSG23 và mã API P08 bị bỏ trống, **không** đánh số lại.
- Các sơ đồ hoạt động và tuần tự chương 2 (c2-04, 06, 08, 09, 11, 12, 15, 16) chưa được rà lại nội dung sau khi gộp use case.
- Ma trận E: ô có nhiều mục ngăn bằng dấu chấm phẩy đã gộp từ nhiều dòng cũ, không tương ứng chính xác từng biến thể.
- Slide chỉ biên dịch phần chương 3; các khung chương 1 và 2 đang bị comment (đã cập nhật mã nhưng chưa bật lại).
- Chương 4, 5, 6 vẫn là khung (`\pending`).
- Cảnh báo căn lề còn lại: tràn 1 pt ở hai dòng tiêu đề bảng ở `c2/gioi-thieu.tex`.

## Quy trình backend từ 09/11/2026

- Người dùng yêu cầu triển khai backend tại `backend/`, sử dụng sub-agent, tự review plan rồi code, không hỏi lại các lựa chọn thông thường.
- Đọc `docs/superpowers/plans/2026-11-09-backend.md` và báo cáo chung `docs/backend/PROGRESS.md` trước khi tiếp tục; kiểm tra Git để không làm lại tính năng đã commit.
- Skill coding đã cài từ obra/superpowers: `~/.codex/skills/writing-plans/SKILL.md`, `~/.codex/skills/test-driven-development/SKILL.md`, `~/.codex/skills/verification-before-completion/SKILL.md`. Dùng đúng nội dung skill khi lập plan/code/kiểm chứng.
- Mỗi tính năng: test hành vi thất bại trước, code, chạy unit và test tích hợp phù hợp, review, cập nhật báo cáo/checklist, commit với trailer `Feature-ID: Fxx`. Root giữ thao tác Git khi có nhiều agent.
- Không đánh dấu pass/done cho test chưa chạy, test bị skip hoặc dịch vụ thật mới chỉ được fake. Báo cáo ghi rõ lệnh, kết quả, giới hạn và bằng chứng.
- Comment code hạn chế, chỉ câu ngắn về vai trò function khi cần. Tham chiếu ADR trong plan khi đặc tả mâu thuẫn; không sửa âm thầm phạm vi báo cáo LaTeX.

- Từ yêu cầu tiếp theo của người dùng: mỗi phần đã verify phải commit và push ngay lên GitLab nhánh `baocao-phucnn`; root thực hiện Git tuần tự. Không sửa migration đã commit, thêm migration số mới cho nâng cấp.
