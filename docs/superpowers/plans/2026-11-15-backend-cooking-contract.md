# UC19 cooking contract implementation plan

> **For agentic workers:** TDD và verification-before-completion; root giữ sharedcontracts/errors/OpenAPI/migration050/Git. Agent pantry sở hữu inventory, agent planning sở hữu meals/http và tests. Đọc interfaces bên dưới trước khi code.

**Goal:** đóng các gap M06 quantity0/lấy ngoài tủ, lotIds, conflict snapshot, cooked actor/time và replay/deductions; giữ FEFO/atomicity/authorization hiện có.

**Architecture:** planning giữ meal transaction và state; pantry tiếp tục sở hữu allocation/movements qua port. Response thêm metadata và nested meal trong khi giữ flat Meal fields hiện có cho client cũ. Replay lấy history qua pantry port, không trừ lại.

**Tech Stack:** Go/pgx/PostgreSQL hiện có, không dependency mới; migration050 chỉ thêm cooked_by nullable.

**Spec:** chapters/c3/api.tex M06/TC-M06-01…05, UC19 ở chapters/c2/pr-ke-hoach.tex; audit trong PROGRESS15/11.

## ADR12 và constraints

- Giữ25UC. Quantity0 chỉ là quyết định lấy nguyên liệu ngoài tủ ở CookLine; pantry Consume vẫn yêu cầu lượng dương. Quantity âm/precision>2 bị400; food phải thuộc công thức/đơn vị cùng dimension/không duplicate kể cả dòng0. Không tạo stockmovement cho0. Dòng0 không được mang LotID/LotIDs nonempty; lựa chọn kho với nguyên liệu ngoài tủ là400, không bị bỏ qua âm thầm.
- CookLine thêm LotIDs []string json:lotIds. Tối đa100 UUIDs hợp lệ/unique, cùng food/space và active/notdeleted; [] dùng FEFO. LotID cũ giữ tương thích, không cho dùng cùng LotIDs nonempty. LotIDs là lựa chọn lô tường minh theo đặc tả, cho phép lô quá hạn đã được người dùng chọn; legacy LotID vẫn cần AllowExpired khi quá hạn.
- Omitted lines giữ tự tính FEFO cho client đang dùng; đây là phần tương thích bổ sung, không thay nghĩa các lines xác nhận. Newclient có thể gửi đầy đủ lines theo M06. SkipStock không gọi Consume.
- Consumption thêm LotIDs []string và Dimension string nhưng StockPort.Available/Consume không đổi. Planning luôn truyền dimension snapshot của nguyên liệu, so khớp explicit UnitID với snapshot trước cả dòng0. Pantry lọc SQL theo desired dimension; IDs được chọn sai dimension bị chặn. Legacy Dimension rỗng chỉ chấp nhận các lô cùng một dimension, mixed-dimension trả400; P05 truyền dimension đã đối chiếu với lô và lọc expiredOnly cùng dimension. New StockHistoryPort.MealDeductions(ctx,tx,sid,mealID)([]StockDeduction,error); StockDeduction gồm FoodID,LotID,UnitID,Dimension string và Quantity Quantity, jsonlowercamel. Quantity là base unit g/ml/cái, UnitID là catalog baseunit của dimension. Inventory implements; planning không đọc schema pantry.
- Cook(ctx,...)(CookResult,error), CookResult embeds Meal giữ flat fields + Details Meal json:meal, Deductions []platform.StockDeduction json:deductions, AlreadyCooked bool json:alreadyCooked. Meal thêm CookedAt *time.Time/CookedBy *string json:cookedAt/cookedBy (không omitempty), SkippedStock bool json:skippedStock. readMeal và collectionHTTP query cùng bổ sungfields; replay cần biết skippedStock.
- Migration050: ADD cooked_by uuid REFERENCES identity.users(id), nullable cho meal cũ. Backfill chỉ khi có activity meal.cooked phù hợp và identity tồn tại; không bịa actor cho lịch sử thiếu. Newcook ghi user/time cùng transaction. Read/list meal planned có cookedAt/cookedBy null. Chưa sửa migration cũ.
- StockHistoryPort là optional capability của StockPort, production inventory luôn implements. Cook có trừ kho yêu cầu history capability để trả metadata đúng (503 nếu thiếu). SkipStock mới hoặc replay đã skipped không cần history và trả[]. Không trả kết quả giả nếu history bắt buộc mà port chưa cấu hình.
- HTTP vẫn cần JSON/IfMatch đúng cú pháp; cooked được xét trước so sánh stale version/validation nghiệp vụ như hiện có; trả AlreadyCooked=true +mealstored/canonical deductions, không actor/time/effect mới. Newcook trảfalse.
- M06 stockshortage chuyển pantry422/2003 thành409/4002 +current snapshot; P05 giữ422. Planning dùng pgx nestedTx/savepoint quanh Consume, rollback savepoint trước khi đọc current để không thấy partialdeductions của chính lượt lỗi. Outer meal/idempotency command vẫnrollback toàn bộ.
- Root thêm StockLevel{FoodID,LotID,Dimension string;BaseQuantity Quantity} jsonlowercamel, StockConflictError{Current []StockLevel}, Error()/Unwrap()->Conflict(); platform.Fail nhận typederror này và bổ sungcurrent. StockLevel.baseQuantity là lượng baseg/ml/cái, lấy Stock.Available hiện có và chỉ cácfood công thức; không lộlot nhóm khác. Error logs vẫn generic.

## Review focus

1. Food0 giữa nhiều nguyên liệu không cómovement; invalid0food/unit/duplicate không lách validation.
2. Multi-lot selection chỉ dùng những lô đã chọn, sort food→expiry/id; expired chỉ tường minh, unknown/foreign/deleted/inactive/duplicate bị chặn; shortage rollbackmọifood.
3. Afterfirstfood consumed then secondfood shortage, current lấy saurollback và meal/lot/movement không bịpartiallycommitted.
4. Concurrent cook/newidempotencykeys chỉ1user/time và1setmovements; replay staleversion không trừ/đổiactor, trả metadata cũ.
5. Owner/member quyền bị revoke trong lúc chờ lock không cómetadata/leak; migrationfromempty vàupgradelegacy cooked null không làm failread/export.

### Task1: pantry selection/history

Files owned inventory/service.go, NEW meal_selection_integration_test.go + optional allocation helper/test; root contracts có ngay.

- [x] Test RED Consumption.LotIDs selection2lots incl expired, onlyselected/foreign/duplicate/bounds andatomicrollback. Implement smallest selection/allocation update, keep Allocate FEFO fuzz invariants unchanged.
- [x] Test RED MealDeductions tenantfilter/stableorder/baseunits/replayandempty. Implement readonlyhistoryquery JOIN ownlots/baseunitcatalog, genericerrors.
- [x] Full inventoryintegration/race, unit/defaultsuite/staticcheck, reportfiles/RED/GREEN; noGit/sharedfiles.

### Task2: planning cook lifecycle

Files owned planning/meals.go,http.go,NEW cook_contract_integration_test.go; relatedexistingtestfixtures only where newsemantics require (reportroot).

- [x] Test RED zeroexternal ingredient +invalidzerofood/unit, list selection conversion, stored actor/time/skipstock, replay withkeydifferent/versionstale andsamehistory.
- [x] Implement CookResult/newMealfields+read/listqueries, Validate0thenexcludeConsume, historycapability andreplay; originalmealfields compatibility preserved.
- [x] Test RED stockshortage rollback+current, nestedTx savepoint; implementtyped409mapping withoutchangingP05. Canonical history afternewcook beforecommit, no cross-schema SQL.
- [x] Full planningintegration/race/defaultsuite/staticcheck; reportroot exactlimits; noGit/migrations/contracts.

### Task3: root contracts/schema/HTTP gates

- [x] Add sharedConsumption.LotIDs/StockDeduction/StockHistoryPort/StockLevel/StockConflictError and050migration. Errorbodycurrenttest RED→GREEN; no oldmigrationediting.
- [x] HTTP OpenAPI exchange quantity0/multilot/metadata/replay/409snapshot RED→GREEN; CookInput and CookResult schemas, Error.current optional; plannedMealcollection nowfields match.
- [ ] Run full make verify integration security/secrets, independentreview andlatestDockerK1–K5+M06metadata checks. UpdatecommonPROGRESS.
- [ ] Commit Feature-ID F28-cooking-contract, push featureGitLab/GitHub then merge main (fetch/mergepreservenewremotecommits, noforce).

Self-review: movementkind consume vẫn giữ để báo cáo/retention hiện có không bị âm thầm đổi sangcook; dữ liệu response gọi deductions. API flatfields và omittedlines là compatibility additions được ghi ADR12, không sửa LaTeX. Quan hệ quyền/FEFO/tiền/lượng chính xác vẫn do modules đảm trách.


## Review bổ sung 16/11/2026

- Review thật qua admin API tái hiện food đổi default unit g→ml trong khi recipe/lô cũ vẫnmass: explicit0/100 và omittedlines đều có thể nhầm dimension. Sửa snapshot check ở planning và desired-dimension filter ở pantry/P05; không chỉ so với catalog hiện tại rồi bỏ qua recipe snapshot.
- History baseunit INNERJOIN từng có thể làm mất deductions sau khi admin bỏ isBase. LEFTJOIN+missingmapping→503, không trả[] giả; newcook rollback khi không thể trả history. Metadata baseunit dùng catalogmapping hiện có, thiếu mapping là lỗi cấu hình chứ không có nghĩa không trừ kho.
- History phải sort theo thuộc tính bất biến food/lot ID; sửa expiry metadata sau cook không được tự đổi thứ tự replay. Allocation vẫn FEFO expiry/id như trước.
- Dòng0 với lựa chọn lô nonempty bị400 để không bỏ qua các IDs chưa kiểm quyền. HTTP vẫn yêu cầu JSON và If-Match hợp lệ; chỉ stale-version comparison được bỏ qua khi món đã cooked.

Latest16/11 review: selected-lot known terminal/zero sau preview là shortage→M06 409/current, không phải404. Unknown/foreign/wrongfood/dimension/deleted vẫn404; LegacyLotID và list đều phải classify trước expiry consent/allocation, không tự chọn lô khác.830Go/8Python và mọi coverage/lint/security gates đã đạt, source freeze chờ Git/runtime.
