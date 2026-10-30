# Thuyết trình chương 1 và chương 2 trong 10 phút

20 slide. Nội dung nói dự kiến 9 phút 40 giây, dự phòng 20 giây chuyển người và chuyển slide. Không tính hỏi đáp.

Có thể chia bốn người: slide 1–5, 6–10, 11–15, 16–20. Đây là gợi ý để nhóm tự phân công.

| Slide | Nội dung | Thời lượng | Mốc kết thúc |
|---|---|---:|---:|
| 1 | Đề tài và thành viên | 15 giây | 00:15 |
| 2 | Quy trình thủ công | 30 giây | 00:45 |
| 3 | Khảo sát và phạm vi | 25 giây | 01:10 |
| 4 | Chu trình nghiệp vụ | 25 giây | 01:35 |
| 5 | Phân rã 8 phân hệ | 20 giây | 01:55 |
| 6 | Tác nhân và kế thừa | 25 giây | 02:20 |
| 7 | Use case tổng quan | 20 giây | 02:40 |
| 8 | Bảng 25 use case | 20 giây | 03:00 |
| 9 | Nhóm gia đình và UC06 | 25 giây | 03:25 |
| 10 | Mua sắm và phân công | 30 giây | 03:55 |
| 11 | UC09 và UC11 | 40 giây | 04:35 |
| 12 | Luồng UC10 | 35 giây | 05:10 |
| 13 | Ví dụ UC10 | 45 giây | 05:55 |
| 14 | Tủ lạnh theo lô | 25 giây | 06:20 |
| 15 | UC14 nhắc hạn | 30 giây | 06:50 |
| 16 | Kế hoạch và UC18 | 35 giây | 07:25 |
| 17 | UC19 xác nhận đã nấu | 35 giây | 08:00 |
| 18 | UC-AI01 hỏi đáp | 40 giây | 08:40 |
| 19 | Hội thoại và danh sách | 40 giây | 09:20 |
| 20 | Chất lượng và kết luận | 20 giây | 09:40 |

## Lời dẫn và điểm nhấn

**Slide 1. Đề tài và thành viên.** Giới thiệu nhóm 7, đề tài và bốn thành viên. Nêu phạm vi báo cáo gồm khảo sát bài toán, đặc tả yêu cầu và luồng hỏi đáp AI tạo danh sách nguyên liệu.

**Slide 2. Quy trình thủ công.** Chỉ ba điểm nghẽn trên ảnh gốc: nhu cầu đến từ nhiều kênh, các thành viên không biết ai đã mua gì, và thực phẩm không được theo dõi hạn dùng. Dẫn đến mua trùng, mua thiếu hoặc bỏ quên thực phẩm.

**Slide 3. Khảo sát và phạm vi.** Các nhóm giải pháp trong khảo sát thường tập trung vào từng khâu. Đề tài liên kết mua sắm với tồn kho và bữa ăn. Không cung cấp thanh toán, giao hàng hay tích hợp tủ lạnh IoT. Hỏi đáp AI là nội dung bổ sung theo yêu cầu trình bày; bảng phạm vi trong báo cáo gốc vẫn xếp trợ lý AI vào định hướng phát triển.

**Slide 4. Chu trình nghiệp vụ.** Đi theo các mũi tên: kế hoạch bữa ăn, phần nguyên liệu cần mua, phân công, nhập tủ, theo dõi hạn dùng, nấu ăn và thống kê. Nhấn mạnh dữ liệu được tái sử dụng giữa các khâu.

**Slide 5. Phân rã 8 phân hệ.** Dùng đúng ảnh gốc. Chỉ các nhánh mua sắm, tủ lạnh, công thức và kế hoạch để giải thích liên kết nghiệp vụ. Không đọc toàn bộ nhãn nhỏ. Tài khoản và nhóm gia đình tách thành hai gói use case ở chương 2.

**Slide 6. Tác nhân và kế thừa.** Có năm tác nhân chính và ba tác nhân phụ. Thành viên kế thừa Người dùng; Trưởng nhóm kế thừa Thành viên. Quản trị viên là tác nhân độc lập, không kế thừa Người dùng, quản lý danh mục và không xem nội dung riêng của gia đình. Bộ lập lịch kích hoạt tác vụ định kỳ.

**Slide 7. Use case tổng quan.** Mỗi elip trên ảnh tổng quan là một gói chức năng, chưa phải một use case đơn lẻ. Người dùng liên quan tới tám gói, còn gói quản trị dành cho Quản trị viên. Liên kết dịch vụ hỗ trợ OTP và thông báo.

**Slide 8. Bảng 25 use case.** Chín gói chứa 25 use case trong báo cáo gốc, gộp từ các thao tác cùng mục tiêu. Có 8 use case tầng 1 đặc tả đầy đủ, 12 tầng 2 chỉ luồng chính và 5 tầng 3 cài đặt tối giản. UC-AI01 là luồng bổ sung trong bộ slide, không tự cộng vào danh mục gốc.

**Slide 9. Nhóm gia đình và UC06.** Giới thiệu vai trò trên ảnh. Người dùng tham gia bằng mã mời hoặc QR; mời và tham gia là hai phía của cùng use case UC06. Mã có tám ký tự, mặc định hiệu lực 48 giờ. Hệ thống kiểm tra mã, nhóm còn chỗ và người dùng chưa thuộc nhóm khác, rồi mới yêu cầu xác nhận tham gia.

**Slide 10. Mua sắm và phân công.** Trưởng nhóm dùng UC08 để giao người mua; các thành viên cập nhật trạng thái chung. Gộp trùng chỉ khi cùng thực phẩm và cùng nhóm đại lượng: 3 lạng cộng 0,5 kg thành 8 lạng. Không tự quy đổi bó rau sang gam.

**Slide 11. UC09 và UC11.** Đọc nửa trái ảnh rồi chuyển sang nửa phải. Nếu mua hộ người khác, ứng dụng hỏi xác nhận. Sau khi ghi nhận đã mua, nhập tủ là lựa chọn mở rộng. Người dùng rà soát hạn dùng và vị trí. Mỗi mặt hàng tạo đúng một lô; yêu cầu gửi lại không tạo trùng.

**Slide 12. Luồng UC10.** Hai phần ảnh liên tiếp mô tả tính nhu cầu theo khẩu phần, phân bổ tồn kho và hàng chờ mua, rồi hiển thị phần thiếu. Chỉ dùng lô còn hạn đến ngày nấu. Người dùng có thể sửa lượng và tạo danh sách mới hoặc gộp danh sách.

**Slide 13. Ví dụ UC10.** Hai bữa cần tổng 1 kg cà rốt. Ngày D lấy 0,4 kg từ lô A. Đến D+2, 0,1 kg còn lại của A đã quá hạn. Lô B và hàng chờ mua chỉ cung cấp 0,3 kg cho nhu cầu 0,6 kg. Vì vậy mua thêm 0,3 kg, không phải 0,2 kg. Đây là lý do phải phân bổ theo ngày nấu.

**Slide 14. Tủ lạnh theo lô.** Mỗi lô lưu lượng còn lại, hạn dùng và vị trí. UC13 gộp sửa lô, ghi nhận lượng đã sử dụng và ghi lượng bị loại bỏ để tính lãng phí. Thực phẩm luôn được nhập tay; quét mã vạch không thuộc phạm vi.

**Slide 15. UC14 nhắc hạn.** Trên ảnh tuần tự, đi từ bộ lập lịch đến dịch vụ, cơ sở dữ liệu rồi thông báo đẩy. Mặc định chạy 8 giờ, nhắc trước ba ngày; người dùng có thể chọn một đến bảy ngày. Thông báo lưu trong hộp thư và không tạo trùng theo ngày. Lỗi đẩy thử lại tối đa ba lần.

**Slide 16. Kế hoạch và UC18.** UC18 giúp chọn món khi lên lịch; từ lịch có thể tạo danh sách mua sắm hoặc xác nhận nấu. Giải thích R là đáp ứng nguyên liệu, E là ưu tiên gần hạn, F là yêu thích. Canh chua đạt 0,75 vì tận dụng cá và dứa gần hết hạn, dù phải mua thêm me. UC18 dùng công thức chấm điểm, khác với hỏi đáp AI.

**Slide 17. UC19 xác nhận đã nấu.** Hệ thống đề xuất lượng trừ theo khẩu phần và FEFO. Người dùng xem trước rồi xác nhận. Nếu kho thay đổi đồng thời, tải lại dữ liệu; nếu gửi lại yêu cầu thì không trừ lần hai. Có thể bổ sung kho hoặc bỏ qua phần trừ khi nguyên liệu lấy từ nguồn ngoài tủ.

**Slide 18. UC-AI01 hỏi đáp.** Người dùng không phải tự liệt kê toàn bộ nguyên liệu. AI hỏi món, số người, ngày nấu và lựa chọn nguyên liệu. Hệ thống đối chiếu tên với danh mục, tính phần thiếu từ tồn kho và hàng chờ mua. Người dùng được sửa và phải xác nhận trước khi lưu. Đây là mô tả tính năng, không khẳng định đã triển khai hay đo hiệu năng.

**Slide 19. Hội thoại và danh sách.** Đóng vai người dùng muốn nấu gà xào nấm, canh rau ngót cho bốn người. AI hỏi có dùng thịt băm cho canh không. Câu trả lời xác nhận món có thịt băm, không có nghĩa tủ lạnh đã có thịt. Bảng minh họa mua 400 g gà, 200 g nấm, 300 g rau và 150 g thịt băm. Các định lượng chỉ minh họa, người dùng được sửa; giả định nguyên liệu trong tủ còn hạn, chưa có hàng chờ mua và gia vị đã đủ.

**Slide 20. Chất lượng và kết luận.** Nhắc các mục tiêu kiểm thử: gợi ý món hai giây, kiểm tra quyền phía máy chủ; ngoại tuyến và đồng bộ thời gian thực là định hướng. Không áp ngưỡng hai giây cho dịch vụ AI. Kết lại với liên kết mua sắm, tủ lạnh và bữa ăn. Cảm ơn thầy và các bạn.

## Câu hỏi có thể gặp

- **Vì sao 8 phân hệ nhưng 9 gói?** Tài khoản và nhóm gia đình tách thành hai gói.
- **FEFO là gì?** Ưu tiên lô hết hạn sớm trong các lô hợp lệ.
- **AI khác UC18 ở đâu?** AI làm rõ nhu cầu qua hội thoại; UC18 xếp hạng món bằng R, E, F.
- **AI có tự ghi danh sách không?** Người dùng xem, sửa và xác nhận trước khi lưu.
- **Có thay đổi báo cáo thành 26 use case không?** Bộ slide giữ nguyên danh mục 25 use case của báo cáo và trình bày UC-AI01 bổ sung riêng. Muốn đồng bộ báo cáo cần bổ sung đặc tả và sơ đồ tương ứng.

## Biên dịch và nguồn ảnh

Nội dung nằm trực tiếp trong `Slide_HUST.tex`, dùng theme và nền HUST gốc. Sơ đồ nhúng trực tiếp từ `figures/diagrams/*.png`; không vẽ lại hoặc tạo ảnh AI. Hai sơ đồ hoạt động dài ở slide 11–12 được chia thành hai vùng hiển thị liên tiếp từ cùng ảnh gốc.

```bash
bash build-slides.sh
```

PDF: `output/pdf/slide-chuong-1-2.pdf`. Cần Tectonic và Arial. Tệp trung gian trong `build/slides/`; PDF và tệp build không đưa vào Git.
