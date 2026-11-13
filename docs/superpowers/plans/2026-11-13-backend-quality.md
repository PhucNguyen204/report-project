# Runtime failure tests, coverage gate and fuzz campaigns

**Goal:** đánh giá coverage theo toàn suite, kiểm đúng lỗi cấu hình/scheduler, thêm fuzz invariants được plan09/11 yêu cầu. Không dùng tỷ lệ để thay assertions nghiệp vụ.

**Architecture:** Go integration dùng coverpkg=./...; parser tổng hợp statement blocks và giữ mọi package trong global denominator. Gate toàn source≥85%, mỗi domain/application package≥85%, kernel/Allocate/Missing/Score≥90%. CLI entrypoints, apicontract/testutil vẫn xuất trong báo cáo, không bị bỏ khỏi global total. Package gate áp app/platform/kernel và6 modules; apicontract/testutil là tooling, cmd là entrypoints.

## Task1: Real failure paths and honest coverage

Files: app/config_test.go, maintenance_integration_test.go, scripts/coverage_report.py +test_coverage_report.py; Makefile và .gitlab-ci.yml.

Interfaces: parse_profile(text)->unique blocks{location:(statementCount,coveredCount)}; summarize(blocks)->packages/global; enforce(summary,coreFunctions)->list failures. CLI nhận profile, chạy go tool cover -func để kiểm3corefunctions và ghi JSON artifact, exit1 nếu gate fail.

- [x] Parser tests RED duplicate blocks chỉ tính một lần, malformed/conflicting counts bị từ chối, missing gated package hoặc core fail không được PASS.
- [x] Chạy gate trên combined profile trước: app81.59% phải FAIL, không nới denominator/threshold.
- [x] Runtime config malformed MODE/listen/URLs/SMTP/version/trusted CIDR phải bị từ chối, không lộ input secret; proxyCIDR hợp lệ và private-key invalid format/length được kiểm.
- [x] Scheduler startup tick ngay, dừng khi context cancel; cleanup error callback chạy rồi cancel, slot rollback và manual next Tick thành công. PostgreSQL statement trigger tạo lỗi có kiểm soát, không giả thành provider smoke.
- [x] make verify/integration: unit Python/Go, race, combined coverage JSON gate; CI lưu report và đọc TOTAL_COVERAGE từ chính summary.

## Task2: Fuzz properties

Files: kernel/quantity_fuzz_test.go, platform/http_fuzz_test.go, inventory/domain_fuzz_test.go, planning/algorithms_fuzz_test.go; Makefile/CI.

- [x] Quantity: mọi int64 Quantity canonical String→Parse giữ đúng; input text bất kỳ parse thành công cũng roundtrip; giới hạn string256byte cho campaign.
- [x] Decode: không panic với JSON arbitrary≤4096byte; success phải là một root object JSON hợp lệ, chỉ field cho phép, quantity giữ chính xác6decimals. Giới hạn input nghiệp vụ2decimals ở validator module, không ở generic Decode.
- [x] FEFO: lot IDs duy nhất/không sửa input; không âm/quá supply, sum đúng demand; auto không dùng expired, explicit chỉ dùng lot chỉ định; thiếu supply không trả partial success.
- [x] Missing: hai demands chung một supply không dùng lại stock; optional bỏ qua, future/expired/wrong-dimension không cấp phát; sum shortQty=max(required-eligible,0), input không đổi.
- [x] Dates: format roundtrip và horizon7..60/ngày biên chính xác; không phụ thuộc timezone máy.
- [x] Chạy từng campaign15s với2workers, capture executions/corpus/failure artifacts. Corpus tìm thấy lỗi phải thành regression và sửa trước commit; không gọi fuzz seeds là campaign đã chạy.
- [x] Full verification/report/commit/push/merge F37-fuzz-campaigns (Git thực hiện ngay sau gate).

Self-review: coverage-combined trước khi thêm tests đã platform86.45%, app81.59%, global85.4%; không lấy phần trăm từng test binary khi coverpkg=./... làm phần trăm package. Không loại code khó test để tăng số. Fuzz không thay DB concurrency, provider smoke hoặc SLA production.

Task1 verification13/11: parser unit5pass; coverage gate baselineFAILapp81.59 then meaningful runtime tests→457Go/0fail/0skip, app88.11/platform86.45/global85.81. All packages remain in global denominator, CLI/tooling reported. Ruby YAML parse CI PASS; remote runner not available. Task2 results bên dưới.

Task2 review: 5 mutants thực tế đều bị seed assertions bắt (mất dấu âm/unknown field/expired FEFO/reuse supply/horizon đảo). Campaign đầu làm lộ oracle JSON sai vì encoding/json nhận case-insensitive field; sửa oracle, giữ biến thể quAntitY làm seed, không đổi API contract. Make3.81 bỏ qua .SHELLFLAGS gây tee che lỗi: Python orchestration test REDreturn0→explicit pipefail→GREEN. Sau fix 5 campaign PASS, executions1678565/175899/652081/411225/361396 (tổng3279166). Corpus hữu ích ở Go cache; crash do oracle sai chuyển thành seed công khai. Không ghi nhận oracle sai là lỗi nghiệp vụ.

Full verification: `make verify integration` PASS, 7 Python tests, 490 Go cases/0fail/0skip; race/vet/mod verify/build đạt; combined global85.83%. Remote CI chưa chạy do runner.
