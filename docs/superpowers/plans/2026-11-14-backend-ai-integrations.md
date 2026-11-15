# AI recommendations and external integration implementation plan

> **For agentic workers:** TDD và verification-before-completion; root giữ migrations/sharedcontracts/router/Git. Chia provider adapter và service khi interfaces dưới đây đã chốt; không hỏi lại lựa chọn thường lệ.

**Goal:** mở rộng UC18 bằng gợi ý AI chạy nền, hoàn thiện cấu hình kiểm thử SMTP/FCM thật khi có credential, giữ nguyên25UC và các luồng hiện có.

**Architecture:** local planner chọn tối đa10candidates hợp lệ; AI chỉ chọn/thứ tự/giải thích bằng aliases, không tạo recipe/food/quantity mới. Request AI opt-in qua endpoint riêng, ghi job/quota và encrypted River event cùng transaction; worker gọi provider ngoài transaction, kiểm lại quyền trước khi lưu. Kết quả TTL24h, chỉ chủ job đang là member được đọc; metadata/log không mang prompt/key/token. Endpoint gợi ý deterministic hiện có không chờ AI.

**Tech Stack:** Go1.26.11/PostgreSQL16/Redis/River/S3; OpenAI Responses +Structured Outputs, official openai-go/v3.99.0 (proxy14/11 xác minh Go1.26), default model `gpt-5.6-terra` (không có snapshot ngày được công bố) khi có key, configurable. SMTP STARTTLS vàFCM HTTPv1/adapters hiện có giữ nguyên.

**Spec:** user14/11 yêu cầu mail/AI/push; UC18 local scoring hiện có và ADR mới bên dưới. Officialdocs đãsearch+open: [Structured Outputs](https://developers.openai.com/api/docs/guides/structured-outputs?api-mode=responses), [Responses create](https://developers.openai.com/api/reference/resources/responses/methods/create), [Go SDK](https://developers.openai.com/api/reference/go), [GPT-5.6 Terra](https://developers.openai.com/api/docs/models/gpt-5.6-terra). Chưa có keytool/credential trong phiên; không gọi OpenAI thật bằng dữ liệu nhóm khi chưa được cấu hình.

## Global constraints

- ADR10: AI là tính năng bổ sung được user authorize14/11, không thay kiểm tồn kho/quyền/tiền/lượng hoặc âm thầm sửa phạm vi LaTeX. AI01/AI02 hỗ trợ UC18, không tạo UC26.
- Không APIkey→AI request503 rõ ràng; không giả kết quả deterministic thành AI. Mailpit/HTTPprovider fixtures không thay smoke SMTP/FCM/OpenAI staging.
- AI chỉ nhận candidatealiases/name/local coverage/missing/expiring names; không gửi user/space IDs/email/token/raw request/freeform prompt. Không tools/web/file uploads; store=false vàkhông previous_response_id.
- SDK retries0, provider timeout20s, bounded output tokens/response parsing, refusal/incomplete/malformed/unknown/duplicate aliases bị từ chối. Business constraints luôn kiểm ở backend.
- Cost quota durable10accepted jobs/user/ngày UTC; idempotency replay không tăng quota; tối đa2 lần claim có thể gọi provider cho AI, không tăng retry ngầm SDK. Không tự claim chi phí hoặc account/model access.
- Group/actor/session guards dùng50377e0. Không network trong transaction; auth revoked/member removed phải chặn enqueue/read/final persistence. Backgroundjob hợp lệ đã chấp nhận có thể tiếp tục sau logout, nhưng actor inactive/admin hoặc mất membership thì không.
- Mỗi feature verify/report/commit/push/main; migration049 mới, không sửa048 trở về trước. Comments tối thiểu.

## Review focus

1. Provider trả alias/recipe không trong candidates, duplicate, refusal/incomplete/oversize/schema sai: job không ready và không mutate nghiệp vụ.
2. Credentials thiếu/invalid/permission mở, provider401/429/5xx/timeout/cancel: lỗi an toàn, bounded retries, không key/prompt trong queue/error/log.
3. Remove member/disable/promote trong lúc HTTP provider chờ: không lưu/đọc kết quả trái quyền, vẫn không giữ DBconnection cho network.
4. Concurrent quota-last-slot/idempotency replay/provider retry: accepted count/job/counter đúng, không gọi provider sau jobready/expired.
5. Job result tham chiếu recipe bị xóa/unpublish sau hoàn tất: Get chỉ trả recipe hiện còn visible; AI không làm thay đổi stock, lịch hoặc công thức.

### Task1: provider boundary and OpenAI adapter

Files create internal/modules/mealai/{provider.go,openai.go,provider_test.go,openai_test.go}; root owns go.mod/go.sum vàcoverage_report.py gate mới.

Interfaces (không thay tên khi service phụ thuộc):
`Candidate{Key,Name string;Coverage float64;Missing,Expiring []string}`;
`Prompt{Date,MealSlot string;Servings,Limit int;Candidates []Candidate}`;
`Choice{Key,Reason string}`;
`Provider.Recommend(context.Context,Prompt)([]Choice,error)`;
`ErrUnavailable` retryable, `ErrRejected`, `ErrInvalidResponse` terminal (generic, không rawprovider response).
`NewOpenAI(apiKey,model string,client *http.Client,baseURL string)(Provider,error)`; baseURL empty dùng officialHTTPS, customHTTP chỉ dùng controlledlocal tests/development config, không auto-read envkey.

- [x] RED tests input/candidate bounds, strictdecode unknown/duplicate/absentalias/reason1–500runes; ownership IDs không có trong providerDTO.
- [x] HTTP fixture kiểm Authorization đúng, model/prompt/schema/storefalse/no tools/boundedbudget và đúngoneAPIcall; completed finalJSON only, skipcommentary, rejectrefusal/incomplete/multipletext/malformed/oversize.
- [x] 401/403/400→ErrRejected;429/5xx/transport/timeout→ErrUnavailable, sanitizederrors/no retries. Adapter dùng officialSDK, schema và business validator đều cóassertions thật.
- [x] Unit/race/modulecoverage≥85%, full regression/security trước commitF38-provider.

### Task2: durable AI job service and protected API

Files create mealai/{service.go,worker.go,http.go,*integration_test.go}; migration049_ai_jobs.sql; app/system.go/lifecycle.go; platform/queue.go; API build.py/openapi.json.

Interfaces: `Planner{Suggest(ctx,user,sid,planning.SuggestionInput)(planning.SuggestionResult,error);GetRecipe(ctx,user,sid,rid,servings)(planning.Recipe,error)}`. `New(db *platform.DB, planner Planner, provider Provider) *Service`; `Request(ctx,user,sid,Input)(Job,error)`; `Get(ctx,user,sid,id)(Job,error)`; `Handle(ctx,platform.Event)error`; `Cleanup(ctx)error`; `Register(*http.ServeMux)`.
Input date/mealSlot/servings/limit theo planner, limit1–10. Job gồm id/status(pending/ready/failed)/expiresAt/items(recipe+reason)/errorCode; không rawprompt/providererror. Aliases r01…r10 chỉ được ánh xạ server-side sang IDs.
POST `/it4788/v1/spaces/{sid}/ai-recommendations`→202 AI01, Idempotency-Key; GET `.../ai-recommendations/{jobId}`→200 AI02. Trả404chooutsider/nonowner/admin; disabledprovider503sauauthz check. Jobquota(counter user/day) atomic upsert trong transaction, FK tenant/owner, LIMITTTLs.

- [x] RED request createsjob+counter+encrypted ai.suggest event atomically, no provider call; rollbackqueuefailure; samekey replayonejob/quota.
- [x] RealPG race lastdaily slot onlyone accepted; bounds/date/role/outsider/owner/expiry errors; workerready/retry/replay/terminal statuses andTTL cleanup.
- [x] Provider paused ngoàiTx: profile/membership mutation cóthểcommit; revoke before finalsave→failed/no accessible result. Planner queried via interface, no cross-schema recipe SQL.
- [x] ReadyGet revalidates recipevisibility; no fabricatedID or stock/meal/recipe mutation; completion notification dùng inbox/FCM port hiện có với allowlisted type.
- [x] APIcontract/providerjob wiring, telemetry kind allowlist, schedulercleanup; full lint/unit/integration/race/security/secret và commitF38-jobs.

### Task3: runtime credentials and staging smoke readiness

Files app/config.go +tests, cmd/server, .env.example/compose, cmd/integration-smoke/{main.go,main_test.go}, README/runbooks/integrations.md; PROGRESS.

- [x] OPENAI_API_KEY_FILE private0400/0600/bounded, OPENAI_MODEL default đã xác minh, missingfile/invalidmodel/baseURL reject without echo; no config→provider nil. Compose đọc file từ .local như keys hiện có, không commit key.
- [x] Smoke script đọc cấu hình, chỉ gửi synthetic email/push/AI khi explicit testrecipient/devicetoken/providerkey có trong local environment; thiếu credential ghi NOT_RUN, không PASS hoặc tự tìm key ngoài repo.
- [x] Hướng dẫn SMTP_ADDR/FROM/USER/PASSWORD/TLS, FCM_PROJECT_ID/CREDENTIALS_FILE+device token, OPENAI_API_KEY_FILE/MODEL. Live destination phải user cấp; không dùng email trongfixtures làm recipient thật.
- [ ] Realnetwork staging evidence khi được cấp key; giữ localprovider fixtures và CIrunner waiting tách rõ. Mỗi phần đãverify commit/push/main.

Self-review: chưa đưa chat tự do, recipe generation, nutrition/medical advice, autonomous cooking/stock changes vào scope. Model cung cấp explanation/ranking, dữ liệu định lượng và lifecycle do backend quyết định. externaltransmission chỉ xảy ra với lời gọi AI riêng được user chủ động yêu cầu.

Model review 14/11: trang official đánh dấu GPT-5 mini và GPT-5.4 nano Deprecated; chọn GPT-5.6 Terra đang hỗ trợ Responses/Structured Outputs. Model có thể cấu hình và phải kiểm quyền truy cập bằng key staging; không khẳng định account đã có quyền hoặc giá cố định.

## Review và bằng chứng 15/11/2026

- Provider đã kiểm bằng HTTP fixture qua official SDK; body256KiB, timeout20s, tokens4096, reasoning none, store=false, không tự đọc key/header môi trường và không retry/redirect.
- Review độc lập bắt hai lỗi thật trước checkpoint: River hết budget nhưng job còn pending; job hết hạn khi chờ khóa vẫn gọi provider. Persistent tests đã RED→GREEN. Event.Attempt/MaxAttempts là metadata nội bộ json:"-", luôn lấy từ River sau decrypt. Lỗi DB ở lượt cuối được snooze để ghi trạng thái khi DB hồi phục; không tăng giới hạn2 claim/provider.
- Lease đang thuộc worker khác được snooze đến hạn, không chiếm hoặc đánh fail nhầm. Kiểm TTL trong khóa job và trước network sau planner; kiểm quyền lại trước publish. Queue failure khi ghi notice rollback trạng thái ready và cho retry.
- Root kiểm tra gateway thật + River + HTTP adapter + inbox. Test contract RED operation chưa được mô tả→OpenAPI AI01/AI02→GREEN. Scope vẫn25UC; hai mã mở rộng ghi riêng extensionCodes.
- Enqueue từng truyền ScheduledAt kể cả tác vụ đã đến hạn, khiến notification chờ scheduler. Test state due=available/future=scheduled RED→GREEN; luồng ready+inbox hiện hoàn tất trong test deadline.
- Config và adapter đã thống nhất ký tự model sau test RED với colon. Thiếu key vẫn503 sau kiểm quyền; CLI smoke mặc định hoặc thiếu cấu hình ghi NOT_RUN.
- Full regression, secret scan và Git checkpoint: root cập nhật kết quả cuối trong PROGRESS.md. OpenAI/FCM/SMTP internet vẫn NOT_RUN đến khi có credential và đích staging; fixture không thay bằng chứng này.
