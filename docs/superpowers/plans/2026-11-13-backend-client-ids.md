# Client resource UUIDv7 consistency

**Goal:** các DTO đã cho phép id client phải giữ UUIDv7 hợp lệ, từ chối version/variant sai400 và collision409/4003, không làm thay đổi dữ liệu khi từ chối. Theo DTO hiện hành, bỏ id/null/chuỗi rỗng là chưa chọn ID và để server sinh; pattern v7 chỉ áp ID thực sự do client cung cấp.

**Spec:** chapters/c3/api.tex:58; tiếp nối F15-client-uuid, không mở rộng offline/delta sync.

## Task1: Shopping resource identities

Files: inventory/shopping.go, http.go, client_ids_integration_test.go, OpenAPI generator.

- [x] RED create list/item chấp nhận UUIDv4; copy list bỏ qua id đã cung cấp; duplicate item ID trước merge không được làm tăng quantity.
- [x] Check optional IDs bằng platform.UUID7 trước mutation; giữ auto ID khi bỏ trống. Duplicate item ID phải được kiểm trước merge, PK vẫn bảo vệ insert race. Cùng food với ID mới vẫn merge theo quy tắc hiện tại, trả canonical existing ID.
- [x] Copy list giữ client ID; test rollback counts/version/activity/source khi ID sai/trùng. Notes override và clear theo input nếu cần DTO copy riêng, có test trước code.

## Task2: Planning resource identities

Files: planning/recipes.go, meals.go, templates.go +client_ids_integration_test.go, OpenAPI generator.

- [x] RED recipe/meal chấp nhận wrong UUID version, template create bỏ qua body.id. HTTP asserts400/409 và không mutation/ingredients/activity/audit khi bị từ chối.
- [x] Validate optional RecipeInput.ID/MealInput.ID; SaveTemplate POST dùng input.id hoặc server UUIDv7; tạo flag/ID trước transaction để retry không nhầm sang update. PUT giữ path ID là định danh, không tự đổi khóa resource.
- [x] Schema input ID pattern v7 cho các DTO create/copy này, không áp cho deviceId/foreign-key/path UUID đọc.
- [x] Unit/HTTP/PG/race/full verify/security; report/commit/push/merge F03-resource-ids.

Self-review: không thêm id vào CopyRecipe nơi contract chỉ nhận name. Không tạo alias mapping client-ID; merge shopping là command và caller vẫn phải giữ Idempotency-Key khi retry. Không sửa migration đã commit hoặc thay phạm vi tài liệu LaTeX.

Verification13/11: HTTP/PG/unit RED wrong version/variant, copy/template ignored ID, duplicate item merge quantity; GREEN sau fix. Template create retry injected55P03 RED no rows→stable creation flag/ID trướcTx→GREEN, đúng1recipe/ingredient/audit. Root make verify integration security440 pass/0fail/0skip, scanner no vulnerabilities. OpenAPI203schemas/101operations, contractDeviceUUIDv4 giữ hợp lệ.
