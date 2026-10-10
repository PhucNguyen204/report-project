# Gợi ý thuyết trình chương 1 và chương 2

13 slide, tổng thời lượng 10 phút, chưa tính hỏi đáp. Có thể chia cho bốn người theo các đoạn: slide 1–4, 5–7, 8–9, 10–13. Đây là gợi ý, không phải phân công đã thống nhất.

| Slide | Nội dung | Thời lượng | Mốc kết thúc |
|---|---|---:|---:|
| 1 | Đề tài và thành viên | 20 giây | 00:20 |
| 2 | Bài toán và khảo sát | 45 giây | 01:05 |
| 3 | Chu trình và phạm vi | 40 giây | 01:45 |
| 4 | Phân rã chức năng | 40 giây | 02:25 |
| 5 | Tác nhân và quyền | 45 giây | 03:10 |
| 6 | Bảng 9 nhóm use case | 45 giây | 03:55 |
| 7 | Biểu đồ use case mua sắm | 45 giây | 04:40 |
| 8 | UC20 và UC21 | 65 giây | 05:45 |
| 9 | UC22 và ví dụ phần thiếu | 70 giây | 06:55 |
| 10 | UC31 nhắc hạn dùng | 40 giây | 07:35 |
| 11 | UC40 gợi ý món | 60 giây | 08:35 |
| 12 | Yêu cầu chất lượng | 35 giây | 09:10 |
| 13 | Tổng kết | 10 giây | 09:20 |

**Dự phòng 40 giây** cho chuyển người nói, chuyển slide và nhấn mạnh ví dụ. Phần nội dung nói dự kiến 9 phút 20 giây, tổng phiên trình bày 10 phút.

## Lời dẫn và điểm nhấn

**Slide 1.** Nhóm 7 trình bày đề tài xây dựng ứng dụng di động đa nền tảng “Đi chợ tiện lợi”. Báo cáo tập trung vào khảo sát bài toán ở chương 1 và đặc tả yêu cầu ở chương 2. Giới thiệu nhanh bốn thành viên trên màn hình.

**Slide 2.** Khi cùng mua sắm cho gia đình, thông tin thường nằm trong tin nhắn và trí nhớ từng người. Vì thiếu trạng thái chung, có thể mua trùng hoặc quên thực phẩm trong tủ. Trong phạm vi khảo sát của nhóm, các giải pháp thường tập trung vào từng khâu. Đề tài đặt mục tiêu liên kết mua sắm, tồn kho và kế hoạch bữa ăn. Không khẳng định mọi ứng dụng thương mại đều thiếu các chức năng này.

**Slide 3.** Đi theo chu trình trên hình: có kế hoạch bữa ăn thì tính phần thiếu cần mua; sau mua hàng thì nhập tủ; hạn dùng giúp chọn món; khi nấu thì ghi nhận tiêu thụ và dùng thống kê cho kế hoạch sau. Đây là ứng dụng quản lý trong gia đình, không phải dịch vụ đặt hàng hay giao hàng.

**Slide 4.** Chỉ giới thiệu tám nhánh, không đọc toàn bộ chữ. Nhấn mạnh các phân hệ mua sắm, tủ lạnh, công thức và kế hoạch chia sẻ dữ liệu với nhau. Tài khoản và nhóm là nền tảng phối hợp, còn tìm kiếm, thống kê và quản trị hỗ trợ vận hành.

**Slide 5.** Hệ thống có năm tác nhân chính và bốn tác nhân phụ. Thành viên nhóm kế thừa người dùng; trưởng nhóm có thêm quyền điều phối, phân công. Khách không kế thừa người dùng. Quản trị viên quản lý dữ liệu danh mục, không được truy cập nội dung riêng của các gia đình. Bộ lập lịch chủ động kích hoạt nghiệp vụ tự động.

**Slide 6.** Tổng cộng 53 use case, chia thành chín gói. Số gói lớn hơn số phân hệ vì tài khoản và nhóm gia đình được tách riêng. Không đọc từng mã. Chỉ chỉ ra mua sắm và tủ lạnh đều có tám use case, rồi dẫn sang gói mua sắm. Báo cáo đặc tả đầy đủ 12 use case trọng tâm, mô tả ngắn 12 use case hỗ trợ và đưa 29 use case còn lại vào phụ lục.

**Slide 7.** Các hình elip biểu diễn use case, khung là phạm vi gói mua sắm. Người dùng thao tác với danh sách và hàng hóa; trưởng nhóm phụ trách phân công. UC18 mở rộng cả tạo danh sách lẫn xem lịch sử. UC21 mở rộng UC20 vì nhập tủ là lựa chọn sau khi mua. Mũi tên extend hướng từ use case mở rộng về use case cơ sở. Liên kết kế thừa quyền của trưởng nhóm đã trình bày ở slide 5, không vẽ lặp.

**Slide 8.** Ví dụ một người được giao mua cà rốt nhưng thành viên khác đã mua hộ. Ứng dụng hỏi xác nhận, sau đó ghi nhận người mua và đồng bộ trạng thái. Khi chọn nhập tủ, tên và lượng đã có sẵn, người dùng rà soát hạn dùng. Hệ thống tạo đúng một lô và chuyển trạng thái mặt hàng trong cùng giao dịch. Nếu thiết bị gửi lại yêu cầu, không tạo lô lần hai. Nhập tủ không phải bước bắt buộc để đánh dấu đã mua.

**Slide 9.** Đây là ví dụ quan trọng nhất: hai bữa cần tổng 1 kg cà rốt nhưng không thể chỉ trừ tổng tồn kho. Ngày D, dùng 0,4 kg từ lô A. Đến D+2, 0,1 kg còn lại của A đã quá hạn. Lô B và hàng chờ mua chỉ cung cấp 0,3 kg cho nhu cầu 0,6 kg. Vì vậy cần mua thêm 0,3 kg, không phải 0,2 kg. Hệ thống xét hạn dùng theo từng ngày nấu và không phân bổ cùng lượng hai lần. Nếu không có nguyên liệu thiếu thì không tạo danh sách.

**Slide 10.** UC31 không cần người dùng bấm chạy. Mặc định 8 giờ mỗi ngày, bộ lập lịch kiểm tra các lô còn lượng và thiết lập của từng thành viên. Ngưỡng mặc định ba ngày nhưng có thể chọn một đến bảy ngày. Mỗi người nhận tối đa một thông báo tổng hợp mỗi ngày. Khi dịch vụ đẩy lỗi, thông báo vẫn ở hộp thư và hệ thống thử lại tối đa ba lần.

**Slide 11.** Gợi ý món sử dụng công thức chấm điểm có quy tắc, không phải mô hình AI đã huấn luyện. Điểm kết hợp mức đáp ứng nguyên liệu, độ gần hạn và sở thích. Ví dụ canh chua thiếu me nhưng sử dụng cá và dứa gần hết hạn nên có điểm 0,75, cao hơn trứng xào cà chua có điểm 0,60. Giải thích rõ: đủ nguyên liệu không phải yếu tố duy nhất. Gợi ý chỉ đọc dữ liệu; khi xác nhận đã nấu bằng UC41, hệ thống mới trừ kho, ưu tiên lô hết hạn sớm và chống trừ lặp.

**Slide 12.** Ngoài chức năng, hệ thống cần đạt các mục tiêu về tốc độ, ngoại tuyến, nhất quán và phân quyền. Các con số hai giây, ba giây là tiêu chí nghiệm thu, chưa phải kết quả đo. Kiểm thử cần chú ý trường hợp nhiều người thao tác đồng thời và gửi lại yêu cầu.

**Slide 13.** Chương 1 xác định bài toán và tám phân hệ. Chương 2 cụ thể hóa thành 53 use case và các yêu cầu chất lượng, làm cơ sở cho thiết kế và triển khai. Cảm ơn thầy và các bạn.

## Câu hỏi có thể gặp

- **Vì sao 8 phân hệ nhưng 9 gói?** Phân hệ tài khoản và nhóm gia đình tách thành hai gói use case.
- **FEFO là gì?** Ưu tiên sử dụng lô có ngày hết hạn gần nhất trong các lô hợp lệ.
- **Có phải gợi ý món dùng AI không?** Đặc tả hiện tại dùng công thức chấm điểm R, E, F, không yêu cầu mô hình học máy.
- **Vì sao không trừ tổng tồn kho một lần?** Tồn kho có thể hết hạn trước một bữa trong kế hoạch, và mỗi lượng chỉ dùng một lần.
- **Tại sao nhập tủ là extend?** Người dùng có thể chỉ ghi nhận mua, không bắt buộc cất vào tủ.

## Chỉnh sửa và biên dịch

Nội dung nằm trực tiếp trong `Slide_HUST.tex`, dùng `beamerthemeHUST.sty` và nền ảnh HUST gốc. Bảng và các sơ đồ rút gọn chỉnh sửa bằng LaTeX/TikZ. Hình chu trình nghiệp vụ lấy từ `figures/diagrams/c1-02-chu-trinh-nghiep-vu.png`; mã PlantUML gốc ở thư mục `diagrams/`.

Chạy từ thư mục gốc dự án:

```bash
bash build-slides.sh
```

Cần Tectonic và font Arial. Lần đầu biên dịch cần Internet để tải gói Beamer. PDF ở `output/pdf/slide-chuong-1-2.pdf`; thư mục `build/slides/` giữ tệp trung gian. Không đưa tệp build vào Git.
