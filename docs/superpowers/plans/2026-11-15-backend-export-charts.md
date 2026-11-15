# UC22 PDF charts implementation plan

> **For agentic workers:** TDD, review và verification-before-completion; agent chỉ sở hữu exports/render.go, file chart.go và tests tương ứng; root giữ report/Git/full regression.

**Goal:** PDF xuất báo cáo có biểu đồ xu hướng theo UC22 bước2; giữ bảng số liệu/CSV và số liệu chính xác.

**Architecture:** tạo chart model từ ReportResult.Series rồi vẽ vector bằng gopdf sẵn có. Chart có nhãn/đơn vị, đặt trước bảng, tái dùng header/footer và pagination hiện có. Không gọi AI/network hoặc render HTML.

**Tech Stack:** gopdf/NotoSans embedded hiện có, Poppler cho kiểm văn bản/raster PDF; không dependency/migration mới.

**Spec:** chapters/c2/pr-thong-ke.tex UC22 bước2; ReportResult.Series trong planning/reports.go.

## Constraints và review focus

- Spending vẽ TotalCost VND; consumption/waste vẽ riêng từng dimension mass(g),volume(ml),count(cái) đang có. Không cộng lượng khác đơn vị vào một trục. PDF vẫn giữ toàn bộ bảng, kỳ trước và thay đổi; CSV không đổi.
- Tối đa8 bars/buckets/biểu đồ: series dài gộp các điểm liên tiếp thành tối đa8bucket cân bằng, nhãn ngày đầu/ngày cuối đọc được, không bỏ đuôi. Tổng bucket dùng integer/fixedpoint với checked overflow; float chỉ dùng tọa độ hình vẽ sau khi dữ liệu đã tính chính xác. Không truncate lượng hoặc tiền nghiệp vụ.
- Chart không có dữ liệu dương thì hiển thị thông báo tiếng Việt rõ ràng, không chia0/NaN/Inf. Biên largeint/precision1micro unit không làm panic. Không dùng tên thực phẩm/tenantID làm file hoặc network.
- Biểu đồ tối đa~240pt cao, width≤520pt; kiểm y/page bounds trước vẽ để không đè header/footer/bảng. Giữ NotoSans chữ Việt và nhãn đơn vị; negative input do callerinvalid phải error hoặc xử lý rõ ràng, không tạo biểu đồ đánh lừa.

### Task1: model và rendering

- [x] Test model literals: exactbucket totals/lastperiod cho365points,8buckets; separate dimensions và values; empty/zero/extreme overflow. Stub→quan sát RED trước implementation.
- [x] Implement model nhỏ trong exports/chart.go, helper vẽ bar chart với nhãn/axis, title/period/units, zero-message. Không thêm reportbusiness queries.
- [x] PDF integration test RED thiếuchart: generate report fixture, pdftotext kiểm title/unit/dates, raster pdftoppm+GoPNG kiểm vùng plot có bars thật (không chỉ nhãn). Kiểm spending/consumption/waste/empty và multi-page365days, preserve finaltable rows.
- [x] Wire encodePDF trước bảng, share pagination callbacks; chạy GREEN/race và exports integration/full default/staticcheck. Viết PDFs/PNGs mẫu vào output/backend-qa/export-charts khi EXPORT_QA_DIR đặt, không commit artifacts.

### Task2: review/checkpoint

- [x] Root đọc PNG trang có chart và trang bảng, kiểm clipping/overlap/đơn vị. Root dùng PDF skill cho read-only visual QA.
- [x] Full make verify integration security +secret scan; cập nhật PROGRESS bằng lệnh/kết quả/giới hạn.
- [x] Commit Feature-ID F31-pdf-charts, push feature GitLab/GitHub rồi merge main. CSV không thay đổi, endpoint/schema không cần đổi.

Self-review: biểu đồ là hình hiển thị từ số liệu đã tính, không dùng float cho tiền/lượng business. Không cần frontend, externalcredentials hoặc thay kiến trúc export async để đóng gap này.
