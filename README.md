# Báo cáo LaTeX – Ứng dụng “Đi chợ tiện lợi” (IT4788)

Báo cáo bài tập lớn môn Phát triển ứng dụng đa nền tảng của **Nhóm 10**, mục lục bám theo mẫu `dsds.docx`.
Hiện đã hoàn thiện **Chương 1** (Khảo sát bài toán), **Chương 2** (Đặc tả yêu cầu bài toán) và các phụ lục liên quan; Chương 3–6 và phần Kết luận vẫn đang ở dạng khung, được ẩn mặc định khỏi bản biên dịch.

## Chạy nhanh trên macOS

Cần có Git và [Homebrew](https://brew.sh). Nếu máy chưa có Git, chạy
`xcode-select --install` và hoàn tất cài đặt Command Line Tools trước.
Repo public nên không cần đăng nhập GitHub để tải mã nguồn.

```bash
git clone https://github.com/PhucNguyen204/report-project.git
cd report-project
./setup.sh
./run.sh --open
```

`setup.sh` cài Tectonic nếu chưa có bộ biên dịch, cùng GNU FreeFont để cung cấp
FreeMono. `run.sh` biên dịch báo cáo và `--open` mở PDF sau khi thành công.
Lần chạy Tectonic đầu tiên cần Internet để tải các gói LaTeX và có thể mất vài phút;
những lần sau dùng lại bộ nhớ đệm.

**PDF đầu ra:** `output/pdf/bao-cao.pdf`. Tệp trung gian và log nằm trong `build/`.
PDF và các tệp trung gian không được lưu trên GitHub; mỗi người chạy lệnh trên
để tạo bản PDF tại máy mình. PDF đầu ra chỉ được cập nhật khi biên dịch thành công.

## Chạy lại sau khi sửa báo cáo

Sửa các tệp `.tex`, lưu lại rồi chạy:

```bash
./run.sh          # tạo PDF
./run.sh --open   # tạo PDF và mở để xem
```

Không cần chạy lại `setup.sh` mỗi lần. Khi cần lấy thay đổi mới từ GitHub,
commit hoặc cất các thay đổi đang làm trước, rồi chạy `git pull --ff-only`.
Script cũng chạy được khi gọi bằng đường dẫn tuyệt đối từ thư mục khác.

### Chọn bộ biên dịch

```bash
./run.sh --tectonic   # chọn Tectonic
./run.sh --xelatex    # chọn XeLaTeX, cần cả xelatex và latexmk trên PATH
```

Mặc định script ưu tiên Tectonic; nếu không có thì dùng XeLaTeX qua `latexmk`.
`make` hoặc `make pdf` tương đương `./run.sh`; `make setup` tương đương `./setup.sh`.
Các lệnh `make watch`, `make clean`, `make distclean` cần cài riêng `latexmk`;
`make watch` cập nhật `build/main.pdf`, còn bản trong `output/pdf/` được cập nhật
bởi `./run.sh`.

### Linux, Windows và Overleaf

- **Linux:** cài TeX Live có XeLaTeX, các gói LaTeX của báo cáo, `latexmk` và font
  cần thiết; sau đó chạy `bash run.sh --xelatex`.
- **Windows:** dùng WSL và làm theo hướng dẫn Linux. Mở `output/pdf/bao-cao.pdf`
  thủ công nếu WSL không có ứng dụng xem PDF.
- **Overleaf:** tải mã nguồn từ GitHub bằng **Code → Download ZIP**, tạo dự án từ
  ZIP, chọn compiler **XeLaTeX** và tệp chính `main.tex`. PDF do Overleaf tạo có
  tên `main.pdf`.

`setup.sh` tự cài công cụ qua Homebrew; không tự cài gói bằng apt hay trình quản lý
gói của Windows. Hướng dẫn macOS đã được chạy kiểm tra trên máy của nhóm;
các môi trường còn lại cần tự chuẩn bị bộ biên dịch và font tương ứng.

## Font chữ

Thân bài ưu tiên **Times New Roman**, tiêu đề ưu tiên **Arial**. Để giữ đúng hai
font này, cần cài chúng vào hệ điều hành trước khi biên dịch; `setup.sh` không tải
Times New Roman hay Arial. Trên máy Mac đã kiểm tra, cả hai font đều có sẵn.

Nếu thiếu, cấu hình sẽ thử TeX Gyre Termes / TeX Gyre Heros. Font mã nguồn được
chọn lần lượt: JetBrains Mono, TeX Gyre Cursor, FreeMono. Font toán được chọn lần
lượt: TeX Gyre Termes Math, STIX Two Math, Cambria Math, Latin Modern Math.
Các font được chọn phải có trên máy hoặc trong bản phân phối TeX.

Kiểm tra các font thực tế được nhúng trong PDF:

```bash
./setup.sh --with-preview
pdffonts output/pdf/bao-cao.pdf
```

## Lỗi thường gặp

| Lỗi | Cách xử lý |
| --- | --- |
| `brew: command not found` | Cài Homebrew, làm theo hướng dẫn thêm vào PATH rồi mở Terminal mới. |
| `Permission denied` khi chạy script | Chạy `chmod +x setup.sh run.sh`, hoặc dùng `bash setup.sh` và `bash run.sh`. |
| `No LaTeX compiler found` | Chạy `./setup.sh`; nếu dùng XeLaTeX, kiểm tra cả `xelatex` và `latexmk` trên PATH. |
| `fontspec Error: The font ... cannot be found` | Cài đúng font được báo thiếu rồi biên dịch lại; FreeMono được cài qua `./setup.sh` trên macOS. |
| Tectonic tải gói thất bại | Kiểm tra kết nối Internet rồi chạy lại `./run.sh`. |
| Biên dịch thất bại sau khi sửa `.tex` | Xem lỗi trong Terminal và `build/main.log`. PDF cũ có thể vẫn còn, cần build thành công để cập nhật. |
| Không thấy logo HUST | Thêm ảnh tại `figures/logo-hust.png`; khi thiếu ảnh, trang bìa hiện khung giữ chỗ. |

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
./setup.sh --with-diagrams  # macOS: cài PlantUML và Graphviz
make diagrams              # kết xuất lại ảnh từ diagrams/*.puml
./run.sh                   # cập nhật PDF với ảnh mới
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

## Đóng góp

Mọi người có thể fork repo, sửa nội dung và gửi pull request vào nhánh `main`.
Thành viên đã được chủ repo thêm làm collaborator và chấp nhận lời mời có thể
push nhánh và merge pull request. Repo public không tự cấp quyền merge cho mọi
tài khoản; gửi username GitHub cho chủ repo nếu cần quyền cộng tác.
