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

# Nguyên tắc cốt lõi
- Không tự ý đưa ra quyết định mà luôn đặt câu hỏi yêu cầu xác nhận từ người thực hiện, trong trường hợp vấp phải những vấn đề mở hoặc mâu thuẫn, nhập nhằng trong đó sự xác nhận trực tiếp của người thực hiện là cần thiết. 

# Quy trình backend từ 09/11/2026

- Người dùng yêu cầu triển khai backend tại `backend/`, sử dụng sub-agent, tự review plan rồi code, không hỏi lại các lựa chọn thông thường.
- Đọc `docs/superpowers/plans/2026-11-09-backend.md` và báo cáo chung `docs/backend/PROGRESS.md` trước khi tiếp tục; kiểm tra Git để không làm lại tính năng đã commit.
- Skill coding đã cài từ obra/superpowers: `~/.codex/skills/writing-plans/SKILL.md`, `~/.codex/skills/test-driven-development/SKILL.md`, `~/.codex/skills/verification-before-completion/SKILL.md`. Dùng đúng nội dung skill khi lập plan/code/kiểm chứng.
- Mỗi tính năng: test hành vi thất bại trước, code, chạy unit và test tích hợp phù hợp, review, cập nhật báo cáo/checklist, commit với trailer `Feature-ID: Fxx`. Root giữ thao tác Git khi có nhiều agent.
- Không đánh dấu pass/done cho test chưa chạy, test bị skip hoặc dịch vụ thật mới chỉ được fake. Báo cáo ghi rõ lệnh, kết quả, giới hạn và bằng chứng.
- Comment code hạn chế, chỉ câu ngắn về vai trò function khi cần. Tham chiếu ADR trong plan khi đặc tả mâu thuẫn; không sửa âm thầm phạm vi báo cáo LaTeX.

- Từ yêu cầu tiếp theo của người dùng: mỗi phần đã verify phải commit và push ngay lên GitLab nhánh `baocao-phucnn`; root thực hiện Git tuần tự. Không sửa migration đã commit, thêm migration số mới cho nâng cấp.

- Từ yêu cầu13/11/2026: push checkpoint đã verify lên nhánh tính năng rồi merge và push nhánh chính. Xác minh origin/HEAD trước khi merge (hiện là `baocao`); không force push. Dùng worktree riêng nếu working tree còn tính năng đang làm. Clone SSH mới cần kế thừa `core.sshCommand` hiện có để đi qua Cloudflare ProxyCommand.

# Quy trình và quy ước wireflow lo-fi trên Figma (chốt 14/11/2026)

File Figma đang làm việc (từ 14/11/2026): `[DiChoThongMinh] Lo-fi wireframe (EDTEAM)`, file key `jpbCt9Dr0IBODHC0MaxKcC`, bản sao trong team Education, truy cập qua Figma MCP bằng tài khoản Education. File gốc `bD7yuqkErsP9l4k7xIRS8b` (gói Starter, hết lượt gọi MCP) chỉ để tham khảo, không vẽ thêm vào đó. ID trang và component mẫu của hai file giống nhau. Quy ước rút ra từ ba trang UC08, UC09, UC10 của Hiếu, rồi chốt lại các điểm chưa nhất quán. Mọi thành viên (Hiếu, Bảo, Mạnh) áp dụng cho trang mới và khi sửa trang cũ.

## Tổ chức trang

- Mỗi use case một trang, tên trang `<Người phụ trách>_<Nhóm chức năng> (UCxx)`. Tên người phụ trách **chỉ** xuất hiện ở tên trang và khung "Thông tin UC", không ghi trên canvas ở chỗ khác.
- Trên cùng là frame **"Thông tin UC"** (rộng 1600 px), bắt buộc đủ: tiêu đề `UCxx – <tên đúng như bảng 25 use case>`; tác nhân; tầng; phụ trách; quy tắc nghiệp vụ (điều kiện, trạng thái, ràng buộc dữ liệu, mã BR); mục "Cách đọc" và dữ liệu mẫu.
- Dưới đó mỗi luồng là một **section**, đọc từ trái sang phải, các section xếp dọc theo thứ tự: Chính, A, B, C…, rồi Ngoại lệ. Ngoại lệ nhiều thì tách thành nhiều section theo chủ đề.

## Đặt tên

- **Section:** tên layer và nhãn chữ trên canvas giống hệt nhau, dạng `UCxx · Chính – <tên luồng>`, `UCxx · A – <tên biến thể>`, `UCxx · Ngoại lệ – <chủ đề>`. Không dùng P1, P2.
- **Màn hình** đánh số theo biến thể, khớp quy ước bước trong báo cáo: luồng chính `UCxx · 01 <tên>`, `02`…; biến thể `UCxx · A1 <tên>`, `A2`, `B1`…; ngoại lệ `UCxx · E01 <tên> (MSGxx, BRxx)`, `E02`… Số ngoại lệ đếm liên tục trong cả trang, kể cả khi chia nhiều section. UC không có luồng chính riêng (như UC10) thì chỉ có A, B…
- **Layer** dạng `Loại · Nội dung`: `Button · Xong`, `Chip · Đang mua`, `Input · Đơn giá`, `Toast · <nguyên văn>`, `Dialog · <tiêu đề>`, `Mặt hàng · Cà chua`, `Checkbox · trống/chọn`.
- **Ghi chú** dưới màn: `Ghi chú · <tên màn>`; mũi tên `Mũi tên · Chạm "<nút>"`; nhãn `Nhãn mũi tên · …`; vùng chạm `Vùng chạm · <layer được chạm>`.

## Bố cục và ký hiệu

- Màn hình 375×812 (iPhone). Trong section, màn đầu đặt ở x = 80, mỗi màn cách nhau 535 px (80, 615, 1150, 1685…), y = 150. Nhãn section ở (80, 40). Ghi chú đặt ngay dưới màn, y = 986, cùng chiều rộng 375.
- **Vùng chạm** là ô viền nét đứt quanh phần tử được chạm. **Mũi tên** đi từ vùng chạm tới cạnh trên của màn kế tiếp, nhãn mũi tên ghi thao tác (`Chạm "Xong"`, `Chọn Đức → "Giao việc"`).
- **Hộp "Chuyển tới"** (nét đứt) dùng khi đích là màn đã vẽ ở chỗ khác hoặc ở trang UC khác. Nội dung ghi `→ UCxx · <số màn> <tên màn>`, ví dụ `→ UC11 · 01 Nhập từ đơn mua`. Không ghi tên người.
- **Màn ngoại lệ** được vẽ như trạng thái tĩnh (lớp phủ + dialog, hoặc toast), không bắt buộc mũi tên. Dòng đầu của ghi chú **bắt buộc** ghi `Rẽ từ: <số màn>, bước <n>` và `Sau đó: về <số màn>` hoặc `Sau đó: kết thúc`.
- Phong cách lo-fi: đen trắng, nút chính nền đen, nút phụ viền. Thông báo dùng nguyên văn từ danh mục MSG của báo cáo, kèm mã MSG trong tên màn.

## Thành phần giao diện

- Thanh tab: không tạo instance mới từ component `tab-bar / iOS / 5-tabs / handwriting` (bản gốc chỉ hiện "Label" và biểu tượng giữ chỗ). Hãy nhân bản thanh tab đã điền sẵn trên màn của Hiếu (node `455:5996`, trang UC19), rồi in đậm nhãn của tab đang mở.
- Vẽ bằng frame thường, không bắt buộc component. Chỉ các instance có sẵn trong trang `[Template]` (icon, Tab bar, Home indicator, Quay lại, Đóng) được dùng dạng instance.
- Bù lại, kích thước phải thống nhất: nút 48 px cao (toàn chiều rộng 343 px), ô nhập 48 px, chip 24 px, dòng mặt hàng 64 px, lề trái 16 px, dialog 311 px rộng đặt giữa màn. Tên layer bắt buộc theo dạng `Loại · Nội dung`.

## Dữ liệu mẫu chung

- Hôm nay: **T2 08/12/2025**. Không gian: **"Nhóm Gia đình"** gồm Hiếu (Trưởng nhóm), Đức, Mạnh. Người đang thao tác mặc định là Hiếu, trừ khi luồng cần vai trò khác.
- Thực phẩm mẫu: thịt ba chỉ, cà chua, rau muống, trứng gà; danh sách mẫu "Đi chợ cuối tuần", ngày mua 13/12/2025. Thêm dữ liệu mới thì giữ nhất quán với các trang đã có.
- Đây là nhân vật trong dữ liệu mẫu, không phải người phụ trách trang. Vì vậy không ghi tên người phụ trách trong hộp "Chuyển tới" hay ghi chú.

## Phạm vi nội dung

- Được vẽ MSG33 (mất kết nối: toast + "Thử lại", thao tác không được lưu) và MSG35 (dữ liệu vừa bị người khác đổi, phát hiện khi lưu bằng kiểm tra phiên bản).
- **Không** vẽ: hàng đợi ngoại tuyến, chỉ báo đồng bộ, cập nhật tự động theo thời gian thực, quét mã vạch (MR04, MR10, NFR10, NFR11 là định hướng hoặc đã bỏ).
- Số màn theo tầng:
  - Tầng 1: luồng chính, mọi biến thể và **mọi nhánh mở rộng** trong đặc tả, kể cả nhánh không có mã MSG (chốt 14/11/2026). Nhánh chỉ là trạng thái (ví dụ lô nổi bật) có thể thể hiện ngay trong màn chính và nêu trong ghi chú.
  - Tầng 2: luồng chính, các biến thể, tối đa 3 ngoại lệ quan trọng nhất.
  - Tầng 3: 1 đến 3 màn chính, không vẽ ngoại lệ.
  - Trang đã vẽ nhiều hơn mức này thì giữ, không xóa bớt.

## Quy ước bổ sung (chốt 14/11/2026)

- **Mã màu hạn dùng (NFR04) trong lo-fi:** ba mức xám, luôn kèm nhãn chữ. Còn hạn = chip nền trắng viền xám; Sắp hết hạn = chip nền xám nhạt viền đậm; Đã hết hạn = chip nền đen chữ trắng. Màu thật để dành cho bản hi-fi.
- **Câu chữ chưa có mã MSG:** người vẽ được đề xuất câu cụ thể, nhưng phải gắn nhãn `[Chưa có MSG – chờ chốt]` trong màn và ghi chú. Các câu này cần được duyệt rồi bổ sung vào Phụ lục C.
- **Biểu đồ trong lo-fi:** vẽ ô giữ chỗ có nhãn (ví dụ "Biểu đồ đường – xu hướng tiêu thụ theo tuần"), không vẽ dữ liệu giả.
- **Header màn có thanh tab:** thêm biểu tượng chuông (hộp thư thông báo) bên trái avatar. Trang của Hiếu chưa có chuông.
- **UC không có luồng chính riêng** (đặc tả chỉ có biến thể A, B như UC10, UC11, UC12, UC21) thì đặt tên section và màn theo A, B.

## Quyết định thiết kế cho các trang của Bảo (chốt 14/11/2026)

Điều hướng chung:
- Thống kê (UC21) mở từ **menu avatar**; thanh tab không thêm tab mới.
- Tìm kiếm (UC20) là **ô tìm toàn cục trên Trang chủ**. Màn Tủ lạnh chỉ có nút bộ lọc.
- Trang chủ chưa ai phụ trách; các trang chỉ vẽ Trang chủ rút gọn với phần tử liên quan (ô tìm, thẻ "Sắp hết hạn", chuông).

Tủ lạnh (UC11–UC14):
- **UC12 màn Tủ lạnh:** dải thống kê nhanh (tổng lô, sắp hết hạn, đã hết hạn), rồi hàng **tab vị trí ngang cuộn** (Tất cả, Ngăn mát, Ngăn đông, Ngăn rau củ, Cánh tủ, Kệ bếp), rồi danh sách thực phẩm gộp theo tên.
- **Nút "Thêm thực phẩm" (UC11-B1):** dòng "+ Thêm thực phẩm" viền nét đứt ngay dưới tab vị trí, giống ô "+ Thêm món" của Hiếu.
- **Xem lô (UC12 bước 4) và thao tác UC13:** chạm thực phẩm mở **màn chi tiết thực phẩm**, gồm tổng lượng và nút "Đã dùng" cấp thực phẩm (trừ theo FEFO). Bên dưới là từng lô, mỗi lô có menu ⋮ gồm Sửa, Dùng lô này (UC13-B3a), Bỏ đi.
- **UC12-B "Sắp hết hạn":** chỉ mở từ thẻ trên Trang chủ và từ thông báo nhắc (UC14). Dải thống kê trên màn Tủ lạnh chỉ để xem.
- **UC11-A:** nút "Cất N mặt hàng đã mua vào tủ" là thanh cố định dưới màn chi tiết danh sách mua sắm, chỉ hiện khi có mặt hàng Đã mua chưa nhập tủ. Màn rà soát là một màn, mỗi mặt hàng là một thẻ có ô Vị trí và Hạn dùng sửa tại chỗ. Sau "Xác nhận" có **màn kết quả riêng**: lô Sắp hết hạn được làm nổi bật, kèm toast MSG10 và hai nút "Xem tủ lạnh", "Về danh sách".
- **Biểu mẫu nhập tay UC11-B và Sửa lô UC13-A:** màn toàn trang. **"Đã dùng" UC13-B và "Bỏ đi" UC13-C:** bottom sheet.
- **UC13 vẽ theo tầng 3** (1 đến 3 màn chính, không vẽ ngoại lệ): 01 Sửa lô, B1 Đã dùng, C1 Bỏ đi.
- **UC14:** vẽ màn hình khóa có thông báo tổng hợp, Trang chủ có chuông, và Hộp thư. Thông báo dẫn tới `→ UC12 · B2`, không vẽ lại UC12-B. Các nhánh 1a, 4a, 5a, 7a, 7b không có giao diện, chỉ ghi trong ghi chú.

Thống kê (UC21, UC22):
- Tab Mua sắm là **một màn đặt đầu section A** (màn mặc định khi mở Thống kê), có ghi chú "tối giản". Section A là Tiêu thụ, B là Lãng phí.
- **Nút "Xuất báo cáo" chỉ có ở tab Lãng phí**, theo điều kiện trước của UC22.
- Chọn định dạng xuất bằng **action sheet hai mục** (PDF có biểu đồ, CSV bảng dữ liệu), chạm là xuất ngay, rồi mở bảng chia sẻ của hệ điều hành.

Mâu thuẫn trong đặc tả cần sửa ở báo cáo LaTeX (chưa sửa):
- `chapters/c2/pr-tu-lanh.tex` dòng 83 ghi UC13 thuộc tầng 3, trong khi dòng 6 và bảng 25 use case ghi tầng 2. Wireflow tạm vẽ UC13 theo tầng 3.
- UC21 bước 5a cho phép xuất báo cáo từ tab Tiêu thụ, nhưng điều kiện trước của UC22 chỉ cho xuất từ báo cáo lãng phí. Wireflow theo UC22.
- UC12 bước 4a đặt thao tác Đã dùng ở cấp lô, còn UC13-B1 đặt ở cấp thực phẩm. Wireflow dùng cả hai: cấp thực phẩm (FEFO) và "Dùng lô này" (B3a).

## Trạng thái các trang của Bảo (14/11/2026, file EDTEAM)

- Đã vẽ xong cả 7 trang theo các quyết định trên: UC11 (16 màn), UC12 (8), UC13 (3), UC14 (3), UC20 (3), UC21 (9), UC22 (5).
- Câu chữ đang gắn `[Chưa có MSG – chờ chốt]`, cần duyệt rồi bổ sung vào Phụ lục C:
  - UC11 · E04: đã nhập tủ từ thiết bị khác.
  - UC11 · E09: xác nhận lưu lô đã quá hạn.
  - UC12 · E01: câu mô tả khi tủ trống.
  - UC21 · E03: thông điệp khích lệ khi không có lãng phí.
  - UC22 · E01: báo lỗi kết xuất.
- Giả định đã xác nhận (15/11/2026): UC11 · E01 "Bỏ qua" giữ mặt hàng ở trạng thái Đã mua.
- Giả định cần chốt:
  - UC12 · E01: "Cất hàng vừa mua" mở UC11 · A2.
  - UC13 · C1: giá trị lãng phí ước tính hiện ngay trong sheet, trước khi xác nhận.

## Quy trình cho mỗi trang

1. Đọc đặc tả UC (tầng 1 ở `chapters/c2/pr-*.tex`, tầng 2–3 ở `backmatter/pl/b-dac-ta-bo-sung.tex`), lấy các bước, biến thể, BR, MSG.
2. Lập frame "Thông tin UC".
3. Vẽ luồng chính, rồi từng biến thể; gắn vùng chạm, mũi tên, nhãn, ghi chú.
4. Vẽ ngoại lệ theo mức của tầng, ghi "Rẽ từ / Sau đó".
5. Tự kiểm tra theo các quy ước trên: tên section, số màn, tên layer, hộp "Chuyển tới", dữ liệu mẫu, phạm vi.

## Việc tồn đọng trên trang của Hiếu (chưa sửa)

- UC08: đổi số màn 05 → A1, 06–07 → B1–B2, 08–09 → C1–C2; cập nhật hộp "Chuyển tới" tương ứng.
- UC08, UC09, UC10, UC19 (và các trang khác của Hiếu): đổi section "P2 – …" thành "Ngoại lệ – …", màn "P2-NN" thành "ENN"; đổi nhãn canvas "P1 · Luồng chính" cho khớp tên layer "Chính".
- UC09: hộp "UC11 (Đức) – Cho vào tủ lạnh" đổi thành `→ UC11 · <số màn> <tên màn>` khi trang UC11 của Bảo có số màn.
- Mọi màn ngoại lệ: thêm dòng "Rẽ từ / Sau đó" vào ghi chú.
- Frame "Thông tin UC" của UC09, UC10, UC19: kiểm tra đủ mục tầng và phụ trách.

# Hi-fi prototype trên Figma (từ 15/11/2026)

File hi-fi: `[DiChoThongMinh] Hi-fi prototype (EDTEAM)`, file key `RlD7y6O8bkxfVWAnRNlSQ6`, truy cập qua Figma MCP bằng tài khoản Education. Ba trang `[Template] … (KHÔNG SỬA VÀO ĐÂY)` là kit gốc (Mobile UI Kit, Components, GreenMaple Logo), không được sửa. Mọi thành viên áp dụng các quy ước dưới đây khi vẽ trang hi-fi.

## Nguyên tắc chung (chốt 15/11/2026)

- **Hi-fi là bản chuẩn.** Lo-fi giữ nguyên như mốc lịch sử, không cập nhật ngược. Chênh lệch giữa hi-fi và lo-fi ghi trong frame "Thông tin UC" của trang hi-fi và trong file này.
- **Thứ tự ưu tiên khi mâu thuẫn:** đặc tả trong báo cáo (`chapters/c2/pr-*.tex`, Phụ lục B, Phụ lục C) > dữ liệu các trang hi-fi đã duyệt > lo-fi. Không tự quyết: liệt kê mâu thuẫn, hỏi người thiết kế, áp dụng phương án được chọn rồi ghi lại. Chỗ nào đặc tả đã quy định rõ thì áp dụng luôn và ghi vào frame thông tin.
- **Câu chữ:** thông báo lấy nguyên văn từ Phụ lục C (`backmatter/pl/c-thong-diep.tex`). Câu chưa có mã MSG giữ nhãn `[Chưa có MSG – chờ chốt]` ngay trong giao diện và ghi chú. Nhãn giao diện mới (ví dụ chip "Riêng của nhóm") ghi vào danh sách cần duyệt.
- **MSG35:** mọi dialog dùng chung tiêu đề "Dữ liệu đã thay đổi", nội dung nguyên văn MSG35 có tên người đổi, nút ghi rõ hành động ("Chọn lại danh sách", "Tải lại bảng xem trước").
- **Người phụ trách:** frame "Thông tin UC" chỉ ghi chủ trang theo tên trang, kể cả khi người khác vẽ hi-fi.

## Bộ component

- Trang `[Template] Exclusive components for DiChoThongMinh` (id `2226:1836`), đặt ngay sau trang Logo. Được phép sửa và bổ sung; khi sửa một component phải kiểm tra lại các màn đang dùng nó.
- Nội dung: 28 icon (Icon/<Tên>, có Heart, Image), 19 bộ component có variant, 16 component đơn: Chip, Button, Tab Pill, Checkbox, Avatar, Icon Button, Home Indicator, Overlay, Field, Stepper, Quantity Input, Dashed Add Row, Option Row, Date Navigator, Sort Button, Tab Item, Tab Bar, App Header, Sub Header, Bottom CTA Bar, Toast, Dialog, Bottom Sheet Header, Banner, Empty State, Stat Strip, Food Row, Shopping Item Row, Meal Card, Week Cell, Review Card, Batch Row, Shortage Row, Ingredient Row, Suggestion Row.
- Thành phần kit dùng trực tiếp: Status Bar `iOS/Status Bar/Black` (`144:330`), Segmented Control/Left (`151:717`, đổi chữ trực tiếp), bàn phím `iOS/Keyboard/Light/Alphabetic` (`144:925`), Header nhóm (`144:165`).
- Màu bổ sung (paint style): Amber/Primary, Amber/Light, Amber/Dark, Red/Primary, Red/Light, Red/Dark, Green/Light, Green/Dark. Không có variable; tô màu bằng paint style.
- Trạng thái dùng chip, luôn kèm nhãn chữ: Còn hạn, Đủ, Đã nấu, Đang mua = Green; Sắp hết hạn, Thiếu = Amber; Đã hết hạn, Không có = Red; Dự kiến, Đang lập, Đã có trong tủ, Riêng của nhóm = Gray. Nút Danger nền Red/Dark.
- Font Inter gán trực tiếp (text style của kit không đọc được font qua API). Ảnh món: khung giữ chỗ Gray/02 với icon Image, không sinh ảnh.
- Màn hi-fi dựng từ instance, không vẽ khung rời. Bộ chọn (vị trí lưu trữ, đơn vị, loại thực phẩm, danh sách đích) dùng bottom sheet.

## Tổ chức trang hi-fi

- Giữ cấu trúc lo-fi: frame "Thông tin UC" rộng 1600 ở (0, 0); dưới đó các section xếp dọc cách nhau 120 px; nhãn section Inter Semi Bold 32 ở (80, 40); màn 375×812 ở x = 80 + 535·i, y = 150; ghi chú rộng 375 ở y = 986.
- Section: `UCxx · A – …`, `UCxx · B – …`, `UCxx · Ngoại lệ – Biến thể A` / `– Biến thể B` (UC có biến thể), hoặc `UCxx · Chính – …` / `UCxx · Ngoại lệ – <chủ đề>` (UC có luồng chính).
- Màn: `UCxx · A1 <tên>`, `UCxx · Exx <tên> (MSGxx)`. Trang lo-fi của Hiếu dùng `P2-NN`: hi-fi đánh số lại thành `Exx` liên tục trong trang, ghi `Lo-fi: P2-NN` trong ghi chú. Màn hi-fi thêm mới (không có ở lo-fi) đặt hậu tố chữ sau màn gốc: `E01b`, `E05b`.
- Frame "Thông tin UC" gồm: tiêu đề `UCxx – <tên>`; dòng "Tác nhân · Tầng ưu tiên · Phụ trách · (Quan hệ) · Bản hi-fi prototype"; "Quy tắc nghiệp vụ" (BR liên quan); "Cách đọc" (tên các flow, điểm chạm giả lập, hộp "Chuyển tới", chú giải màu, dữ liệu mẫu); "Quyết định riêng của bản hi-fi (ngày)".
- Ghi chú dưới màn `Ghi chú · <tên màn>`: màn ngoại lệ mở đầu bằng `Rẽ từ: … Sau đó: …`; tiếp theo các gạch đầu dòng về bước, BR, MSG; dòng `Liên kết: <phần tử> → <màn>`; dòng `Giả lập: …` nếu có điểm chạm giả lập; `Lo-fi: …` nếu đổi số hoặc khác lo-fi.
- Hộp "Chuyển tới" (nét đứt, nền trắng) đặt cạnh phải màn, ngang phần tử được chạm, gồm dòng `Chạm "<nút>"` (thêm `Exx ·` phía trước nếu hộp không nằm ngay cạnh màn nguồn) và `→ UCxx · <số màn> <tên màn>`. Figma không nối được liên kết prototype giữa hai trang, nên mọi đích ở trang UC khác (kể cả trang đã vẽ hi-fi) đều dùng hộp này.

## Bố cục màn

- Status Bar ở (0, 0) luôn nằm trên cùng. Màn có thanh tab: App Header ở y = 44 (không gian, chuông, avatar, tiêu đề 30), Tab Bar ở y = 728. Màn con và biểu mẫu: Sub Header ở y = 44 (icon Close hoặc Chevron Left), nội dung từ y = 104, lề 16, khoảng cách 12 đến 16.
- Thanh nút cố định: Bottom CTA Bar sát đáy (nút chính, nút phụ kiểu Secondary hoặc Ghost, Home Indicator). Toast đặt cách thanh dưới 12 px. Màn không có thanh dưới thì đặt Home Indicator ở y = 778.
- Màn cuộn: nội dung là frame auto-layout dài hơn 812; frame màn đặt `overflowDirection = VERTICAL` và `numberOfFixedChildren` bằng số layer cố định nằm trên cùng (Status Bar, thanh dưới, toast, overlay, sheet). Hàng tab cuộn ngang: frame clip `overflowDirection = HORIZONTAL` chứa hàng auto-layout; muốn hiện tab ở cuối thì dịch hàng `x = 375 − width`.
- Trạng thái overlay (bottom sheet, dialog) vẽ thành màn riêng: nhân bản màn nền, thêm Overlay rồi sheet (bám đáy, `y = 812 − height`) hoặc Dialog (giữa màn). Liên kết điều hướng sang màn đó, không dùng overlay của prototype.
- Màn dùng lại nền của màn khác thì nhân bản màn đã dựng (`clone`) trước khi nối liên kết, để giữ dữ liệu đồng nhất.

## Liên kết prototype

- Mỗi biến thể có một flow starting point tên `UCxx · A – <tên>`, `UCxx · B – <tên>`. Ngoại lệ không có lối vào từ luồng chính thì có flow riêng `UCxx · Exx <tên>`.
- Ngoại lệ có lối vào hợp lý thì mở bằng điểm chạm giả lập (ví dụ chạm ô Hạn dùng → màn lỗi MSG14); màn lỗi có điểm chạm quay về màn gốc. Ghi rõ ở dòng "Giả lập".
- Chuyển cảnh (EASE_OUT): sang màn sâu hơn `MOVE_IN` trái 0,3 s; quay lại `MOVE_OUT` phải 0,3 s; mở biểu mẫu hoặc màn con từ đáy `MOVE_IN` 0,3 s, đóng `MOVE_OUT` xuống đáy; đổi trạng thái, mở sheet, dialog, màn lỗi `DISSOLVE` 0,2 s.
- Gắn liên kết lên phần tử thật (nút trong Bottom CTA Bar, ô Field trong Review Card, `leading` của Sub Header, `close` của sheet, Overlay). Kiểu thiết bị trình chiếu (iPhone) phải chọn tay trong bảng Prototype.

## Quy trình cho mỗi trang hi-fi

1. Đọc trang lo-fi (lấy toàn bộ chữ bằng script chỉ đọc, chụp từng section), đặc tả UC trong báo cáo, MSG trong Phụ lục C, và sổ dữ liệu mẫu bên dưới.
2. Liệt kê mâu thuẫn giữa lo-fi, đặc tả và các trang hi-fi đã duyệt; hỏi người thiết kế trước khi vẽ (tối đa 4 câu mỗi lượt, có phương án khuyến nghị).
3. Dựng frame "Thông tin UC" và section; dựng màn theo section; chụp kiểm tra từng section và sửa lỗi hiển thị (chữ tràn, xuống dòng, sheet lệch đáy).
4. Nối liên kết và flow; đọc lại số liên kết đã lưu.
5. Cập nhật file này: số màn, liên kết, flow, quyết định khác lo-fi, việc còn mở, thay đổi trên trang component.

## Sổ dữ liệu mẫu hi-fi (giữ nhất quán giữa các trang)

- Hôm nay T2 08/12/2025; "Nhóm Gia đình" (Hiếu – Trưởng nhóm, Đức, Mạnh); người thao tác mặc định Hiếu; ngưỡng nhắc N = 3.
- Danh sách mua sắm:
  - "Đi chợ cuối tuần": dự kiến mua 13/12/2025, Đang mua. Rau muống 2 bó (chưa mua); Thịt ba chỉ 0,5 kg (Hiếu mua 17:25, 60.000 ₫); Cà chua 1 kg (Hiếu mua 17:30, 25.000 ₫). Hai mặt hàng đã mua được cất vào tủ ở UC11-A.
  - "Chợ đầu tuần": dự kiến mua hôm nay, Đang mua.
  - "Mua cho tuần 08/12 – 14/12": tạo ở UC10-A, dự kiến mua 08/12/2025, Đang lập: Cá basa 0,5 kg, Cá lóc 0,5 kg, Me 30 g, Cà rốt 0,3 kg.
- Tủ lạnh (UC11 B1, 9 lô): Đậu phụ 2 bìa HSD 07/12 (Đã hết hạn); Cà chua 1 kg, 2 lô, HSD gần nhất 09/12; Sữa tươi 2 hộp HSD 10/12; Thịt ba chỉ 0,5 kg Ngăn mát HSD 11/12; Rau muống 2 bó HSD 12/12; Trứng gà 10 quả HSD 15/12. Sau UC11-A có thêm Cà chua 1 kg Ngăn rau củ HSD 15/12. Cá lóc 500 g sắp hết hạn (còn 1 ngày) dùng ở UC10.
- Hạn gợi ý: cà chua Ngăn rau củ 7 ngày; thịt miếng Ngăn mát 3 ngày, Ngăn đông 120 ngày; sữa tươi Cánh tủ 7 ngày. Thực phẩm riêng mới tạo chưa có hạn gợi ý.
- Công thức "Canh chua cá lóc": 30 phút, bữa trưa, tối, chuẩn 2 khẩu phần; ở 4 khẩu phần: Cá lóc 500 g (Đủ, sắp hết hạn), Dứa 200 g (Đủ), Cà chua 300 g (Đủ), Me 30 g (Không có), Giá đỗ (tùy chọn) 100 g (Không có).
- Lịch tuần 08–14/12: Sáng T2 Bánh mì; Trưa T2 Trứng xào, T3 Rau xào, T6 Canh rau (thiếu); Tối T2 Thịt kho, T3 Cá kho (thiếu Cá basa), T4 Canh chua (thiếu), T5 Trứng xào, T7 Ăn ngoài (không gắn công thức).
- Thực phẩm riêng của nhóm: "Sữa tươi thanh trùng" (Trứng và sữa, tạo ở UC11-E05).
- Hai lô cà chua cũ: 0,4 kg HSD 09/12 và 0,6 kg HSD 14/12 (dùng ở UC19). Trứng gà 10 quả là một lô HSD 15/12.
- Lịch ngày T2 08/12 (UC19): Sáng "Bánh mì" 2 khẩu phần, Đã nấu; Trưa "Trứng xào cà chua" 3 khẩu phần (cần 0,5 kg cà chua, 4 quả trứng); Tối "Thịt kho" 4 khẩu phần, Dự kiến.
- Màn ngoại lệ được phép dùng trạng thái tủ giả định khi sổ dữ liệu không tạo ra được nhánh đó; ghi rõ "giả định" trong ghi chú và frame thông tin (UC10 · E03 Nước dừa; UC19 · E01, E01b).

## Lưu ý kỹ thuật khi dựng bằng Figma MCP

- Gọi `create_session` trước; trước `use_figma` đọc skill qua MCP resource `skill://figma/figma-use/SKILL.md` (và `figma-generate-design` khi dựng màn, `figma-generate-library` khi sửa component), truyền `skillNames` dạng `resource:…`.
- Mỗi lần gọi chỉ chuyển trang một lần. Lần gọi lỗi được hoàn tác toàn bộ, nhưng vẫn đọc lại canvas trước khi chạy lại.
- Thuộc tính chữ của một bộ variant dùng chung giá trị mặc định, nên trang trưng bày hiện cùng một chữ ở mọi variant; khi dùng instance phải đặt lại. Đặt thuộc tính bằng `setProperties` với khóa tìm theo tên (phần trước dấu `#`).
- Instance lồng được expose (nút trong Bottom CTA Bar và Dialog, Field trong Review Card, chip trong Ingredient Row, Quantity Input trong Suggestion Row và Batch Row, nút trong Empty State) được đặt thuộc tính trực tiếp. `isExposedInstance` chỉ đặt được sau khi instance đã nằm trong component.
- Field hiển thị giá trị một dòng, cắt bằng dấu ba chấm: giữ giá trị ngắn. Chữ giữ chỗ dùng State Empty, hoặc đổi màu chữ giá trị sang Gray/03.
- Sau khi đổi kích thước một component, kiểm tra các sheet đang bám đáy (đặt lại `y = 812 − height`).

## UC11 hi-fi (Bảo, xong 15/11/2026)

- Trang `Bảo_Quản lý thực phẩm trong tủ lạnh (UC11)` (id `2223:1836`): 19 màn, 35 liên kết, 4 flow (A, B, E01, E04).
- Quyết định khác lo-fi:
  - A1 dùng nhãn "Dự kiến mua: 13/12/2025" thay "Ngày mua" (áp dụng cho mọi trang có danh sách mua sắm).
  - E01 "Bỏ qua": mặt hàng vẫn ở trạng thái Đã mua (đã xác nhận). Thêm màn E01b.
  - E05 đồng bộ theo từ khóa "sữa t" (Sữa tươi thanh trùng, Trứng và sữa). Thêm màn E05b; Ngày hết hạn để trống, bắt buộc nhập.
  - E09 "Vẫn lưu" dẫn tới màn E09b (lô Đã hết hạn).
- Food Row kiểu Card: chip nằm dưới dòng thông tin (đã sửa trên trang component).
- Còn mở: nhãn chip "Riêng của nhóm" (E05b) cần duyệt; "Chọn thực phẩm" ở E01 chưa có màn đích; "Xác nhận" ở E01b và "Lưu" ở E05b chưa có màn kết quả riêng; "Xem tủ lạnh", "Về danh sách" chờ trang UC12, UC09 hi-fi.

## UC10 hi-fi (vẽ 15/11/2026, trang của Hiếu)

- Trang `Hiếu_Quản lý danh sách mua sắm (UC10)` (id `227:103`): 10 màn, 15 liên kết, 5 flow (A, B, E01, E03, E04). Ngoại lệ đánh số lại E01–E04 (lo-fi P2-01 → P2-04), tách section theo biến thể.
- Đã duyệt, khác lo-fi:
  - Ngày dự kiến mua đề xuất 08/12/2025 theo bước 5 (lo-fi 09/12).
  - Danh sách đích mặc định ở B2 là "Chợ đầu tuần" (ngày dự kiến mua gần nhất, bước B2).
  - Dữ liệu đồng bộ UC11: Cá kho cần "Cá basa 0,5 kg" thay Thịt ba chỉ; Cà chua ở công thức là Đủ; "Đi chợ cuối tuần" Đang mua.
  - Sheet biến thể B không cho sửa số lượng (bước B3).
- Áp dụng theo đặc tả: A2 hiện banner món không gắn công thức (bước 2a).
- Thêm icon Heart, Image vào bộ component. Bottom Sheet Header cho tiêu đề và phụ đề tự xuống dòng.
- Còn mở: "Gộp" ở E02 và "Thêm" ở E03 chưa có màn kết quả riêng; "Mở danh sách" (B3) chờ trang UC09 hi-fi. Cần báo Hiếu các thay đổi dữ liệu và đánh số.

## UC19 hi-fi (vẽ 15/11/2026, trang của Hiếu)

- Trang `Hiếu_Quản lý kế hoạch bữa ăn (UC19)` (id `227:107`): 8 màn, 13 liên kết, 4 flow (Chính, E01, E03, E04). Ba section: Chính (01–03); Ngoại lệ – Thiếu tồn kho và lô hết hạn (E01, E01b); Ngoại lệ – Bỏ qua trừ kho, xung đột, đã nấu ở thiết bị khác (E02–E04). Lo-fi P2-01 → E01, P2-02 → E02, P2-03 → E03.
- Đã duyệt, khác lo-fi:
  - Bữa sáng T2 là "Bánh mì" Đã nấu (đồng bộ lịch tuần UC10); lo-fi để trống.
  - E01, E01b dùng trạng thái tủ giả định (Cà chua còn 0,3 kg; Trứng gà chỉ còn lô HSD 06/12).
  - Thêm E01b (4a "Bỏ qua": 0,2 kg dùng nguồn ngoài tủ, có Hoàn tác) và E04 (6b: Đức đã xác nhận nấu lúc 11:40, banner).
  - 5a chỉ dùng toast MSG10; thẻ món ghi "Không trừ kho".
  - Dialog MSG35 tiêu đề "Dữ liệu đã thay đổi".
- Câu `[Chưa có MSG – chờ chốt]` mới: UC19 · E01b ("0,2 kg dùng nguồn ngoài tủ."), UC19 · E04 ("Đức đã xác nhận nấu món này lúc 11:40 – không trừ kho lần nữa.").
- "Bổ sung tồn kho" dùng hộp "Chuyển tới → UC11 · B2". Cần báo Hiếu các thay đổi dữ liệu và đánh số.

## Việc chờ ở các trang hi-fi tiếp theo

- Khi vẽ UC08, UC09: dùng nhãn "Dự kiến mua"; các nút dẫn sang UC10, UC11 dùng hộp "Chuyển tới" (không liên kết được giữa các trang).
- Danh sách câu chờ chốt của bản hi-fi cần duyệt rồi bổ sung Phụ lục C: UC11 · E04, UC11 · E09, chip "Riêng của nhóm" (UC11 · E05b), UC19 · E01b, UC19 · E04.
