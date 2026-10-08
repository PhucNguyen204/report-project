# Báo cáo LaTeX – Ứng dụng “Đi chợ tiện lợi” (IT4788)

Báo cáo bài tập lớn môn Phát triển ứng dụng đa nền tảng, mục lục bám theo mẫu `dsds.docx`.
Hiện đã hoàn thiện **Chương 1** (Khảo sát bài toán), **Chương 2** (Đặc tả yêu cầu bài toán) và các phụ lục liên quan; Chương 3–6 và phần Kết luận vẫn đang ở dạng khung, được ẩn mặc định khỏi bản biên dịch.

## Biên dịch

Báo cáo dùng **Tectonic** (dựa trên XeTeX) hoặc **XeLaTeX**, hỗ trợ tiếng Việt và phông chữ hệ thống.

### Chạy trên máy cá nhân

```bash
./setup.sh          # cài Tectonic và GNU FreeFont (FreeMono) bằng Homebrew
./run.sh            # xuất output/pdf/bao-cao.pdf
./run.sh --open     # biên dịch và mở PDF
```

Script chạy được cả khi gọi từ thư mục khác. Lần đầu dùng Tectonic cần Internet
để tải các gói LaTeX; những lần sau dùng lại bộ nhớ đệm. Tệp trung gian và log
nằm trong `build/`; PDF đầu ra chỉ được cập nhật sau khi biên dịch thành công.
Xem `build/main.log` nếu gặp lỗi hoặc cảnh báo.

Tùy chọn: `./setup.sh --with-preview` cài Poppler để xem/kiểm tra PDF;
`./setup.sh --with-diagrams` cài PlantUML và Graphviz để dựng lại sơ đồ.
Nếu đã cài TeX Live/MiKTeX, có thể chọn `./run.sh --xelatex` (cần `latexmk`
và `xelatex` trên PATH). `./run.sh --tectonic` chọn Tectonic.

Trên Linux/Windows, cài Tectonic hoặc TeX Live bằng trình quản lý gói phù hợp;
chạy script bằng Bash (Windows có thể dùng WSL).

### Overleaf và Make

- **Overleaf**: tải cả thư mục lên, chọn *Menu → Compiler → XeLaTeX*, tệp chính `main.tex`.
- **Máy cá nhân** (TeX Live / MiKTeX):

  ```bash
  make            # tương đương: ./run.sh
  ```

Phông chữ: ưu tiên Times New Roman (thân bài) và Arial (tiêu đề) như mẫu Word; nếu máy không có, preamble tự chuyển sang TeX Gyre Termes / TeX Gyre Heros. Mã nguồn dùng JetBrains Mono hoặc phông đơn cách tương đương có hỗ trợ tiếng Việt. Phông toán: TeX Gyre Termes Math, STIX Two Math hoặc STIX Math.

Trên máy Mac hiện tại, Times New Roman và Arial đã có sẵn; `setup.sh` cài thêm
GNU FreeFont để cung cấp FreeMono theo cấu hình dự phòng của bản gốc.
Có thể kiểm tra font thực tế được nhúng bằng `pdffonts output/pdf/bao-cao.pdf`
(cần `./setup.sh --with-preview`).

## Cấu trúc thư mục

```
main.tex                  tệp chính
setup.sh                  cài công cụ biên dịch (macOS/Homebrew)
run.sh                    biên dịch và xuất PDF
output/pdf/bao-cao.pdf     báo cáo sau khi biên dịch
build/                    tệp trung gian và log
config/preamble.tex       bố cục trang, phông, tiêu đề, mục lục, đầu/chân trang
config/ucspec.tex         môi trường bảng: ucspec, fieldtable, nvtable, ddtable
frontmatter/              trang bìa, thuật ngữ, lời nói đầu, phân công
chapters/chuong1.tex      Chương 1
chapters/chuong2.tex      Chương 2 (nội dung chia trong chapters/c2/*.tex)
chapters/chuong3..6.tex   khung các chương sau
backmatter/               kết luận, tài liệu tham khảo, phụ lục (backmatter/pl/*.tex)
diagrams/*.puml           mã nguồn PlantUML của mọi sơ đồ (_style.iuml là kiểu dáng chung)
figures/diagrams/*.png    ảnh sơ đồ đã kết xuất (được \includegraphics)
figures/logo-hust.png     (tùy chọn) logo trên trang bìa – chưa có thì hiện khung giữ chỗ
```

## Sơ đồ PlantUML

Sau khi sửa tệp `.puml`, kết xuất lại:

```bash
make diagrams           # cần Java + plantuml (+ Graphviz cho sơ đồ use case/lớp/trạng thái)
```

Nếu máy không có Graphviz, thêm dòng `!pragma layout smetana` vào đầu `diagrams/_style.iuml`
(bố cục sẽ kém gọn hơn một chút). Ảnh PNG đã được kết xuất sẵn nên không bắt buộc chạy bước này để biên dịch PDF.

## Quy ước khi viết tiếp

- Mã tham chiếu: `\uc{UC01}` use case, `\br{BR01}` quy tắc nghiệp vụ, `\msg{MSG01}` thông điệp, `\nfr{NFR01}` yêu cầu phi chức năng.
- Chèn sơ đồ: `\diagram[độ rộng]{tên-tệp-png}{Chú thích}{fig:nhan}`.
- Đặc tả use case theo mẫu học phần:

  ```latex
  \begin{ucspec}{UC01}{Đăng ký tài khoản}
  \ucfield{Tác nhân}{Khách}
  \ucflow{Luồng thực thi chính}
  \ucstep{1}{Khách}{Chọn chức năng “Đăng ký”.}
  \ucflow{Luồng thực thi mở rộng}
  \ucstep{3a}{Hệ thống}{...}
  \ucfield{Điều kiện sau}{...}
  \end{ucspec}
  ```
- Hiện các chương đang là khung để tiếp tục biên tập: đổi `\showpendingfalse` thành `\showpendingtrue` trong `main.tex`.
- Cột “Điện thoại”, “Tổng hợp công việc”, “Đánh giá” trong `frontmatter/phan-cong.tex` để nhóm tự điền.
