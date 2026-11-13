# Backend keyset pagination implementation plan

> Root thực hiện theo yêu cầu triển khai tự động, TDD, review, commit/push từng task. Tiếp nối F03/F16/F18/F22/F24/F33/F34.

**Goal:** danh sách không bị cắt ở trang đầu, default20/max100, cursor ổn định theo tuple sắp xếp và id.

**Architecture:** platform đọc limit/cursor và sinh metadata; module giữ SQL/scope/filter/order. Cursor base64url chứa version, fingerprint request và sort keys; fingerprint gồm method/path/user/filter, bỏ limit/cursor. Cursor không cấp quyền; mọi query vẫn kiểm membership/admin.

**Tech Stack:** Go1.26.11, PostgreSQL16, net/http, JSON/base64url.

**Spec:** `chapters/c3/api.tex:58`, S01/R01 và P01/Q01 tại `backmatter/pl/f-dac-ta-api.tex`; backend plan09/11.

## Constraints and review focus

- Giữ envelope data.lots/data.lists/data.items, thêm meta.nextCursor/serverTime; pantry giữ data.nextCursor để tương thích.
- Tối đa100, mặc định20, sai limit/cursor trả400 trước SQL; UUID/date/time cursor được kiểm kiểu.
- Giữ scope/filter khi phân trang; không OFFSET. Tuple có id để tránh mất bản ghi trùng sort key. Chèn trước cursor không làm lặp bản ghi đã đọc.
- Facet và tổng tháng tính trên toàn tập lọc, không chỉ trang hiện tại. Metadata không được dùng thay assertions nghiệp vụ.
- Không sửa migration đã commit; thêm044 nếu cần index. Các collection cố định/bounded (members≤10, spaces≤2, catalog cấu hình, ingredients và suggestions≤10) giữ hợp đồng riêng.
- Q01 là search ba loại có facets, cần audit/filter/cursor riêng; không âm thầm gọi đó là complete vì sửa các list khác.

## Task1: Common cursor and inventory

Files: `internal/platform/pagination{,_test}.go`, `internal/modules/inventory/{http.go,pagination.go,pagination_integration_test.go}`, `api/build.py`, `api/openapi.json`, migration044.

Interfaces: `ReadPage(*http.Request,...PageKey)(Page,error)`; Page{Limit,After}, Page.Next(...string)string; keys PageUUID/PageDate/PageTime/PageText; `WritePage(w,data,next,now)`.

- [x] Unit RED: default20/max100, duplicate/invalid params, bad base64/type/arity/version, filter/path/user binding, limit resize giữ cursor hợp lệ.
- [x] Implement strict cursor parser/metadata. Unit GREEN.
- [x] Inventory HTTP RED:21+ lots/lists, default20 và cursor lấy đủ; tie plannedDate, insert trước cursor không lặp, malformed/cross-filter cursor400, scope vẫn404.
- [x] P01/expiring SQL lọc và keyset id, facets toàn tập lọc. S01 plannedDate DESC/id ASC, aggregate itemCount/totalCost/purchasedBy; groupBy=month có tổng đúng toàn filter, overflow trả lỗi.
- [x] Cập nhật schema/default20/meta/cursor; actual HTTP contract và full integration/race/verify. Review/báo cáo/commit/push F16-pagination.

## Task2: Planning collections

Files: `internal/modules/planning/{http.go,http_extra.go,pagination_integration_test.go}`, OpenAPI generator/artifact.

- [ ] RED R01 default20/name+id tie/favorite/source, chèn trước cursor, đủ mọi trang; M01 date+slot+id, giới hạn theo filter ngày.
- [ ] R01 keyset name ASC/id ASC; M01 date ASC/slot ASC/id ASC; admin templates name/id. Parse cursor trước SQL, giữ quyền trong service.
- [ ] Update schemas; full tests/review/report/commit/push F24-pagination.

## Task3: Administration and remaining audit

Files: `internal/modules/accounts/{admin.go,catalog.go,pagination_integration_test.go}`, OpenAPI generator/artifact.

- [ ] RED D01/D10 default20/time+id tie, cursor không đổi filter/user, nonadmin403; catalog admin name/id với21+ entries.
- [ ] SQL keyset users/audit createdAt DESC/id DESC, catalog name/id ASC; giữ field_versions nội bộ không trả ra ngoài.
- [ ] Rà Q01/search filters, lịch sử stock movement và hoạt động; ghi gap cụ thể hoặc implement theo test trước khi đóng coverage requirement.
- [ ] Full tests/security/contract, report, commit/push F34-pagination. CI remote không tự tick nếu chưa có runner result.

Self-review: không thay generic search/suggestion thành list một cách vô ý; keyset bảo đảm không lặp do insert trước cursor, không hứa snapshot qua mọi lần đổi sort key. Mọi caller vẫn kiểm quyền và scope; aggregate theo full filters tránh tổng tiền phụ thuộc page size.
