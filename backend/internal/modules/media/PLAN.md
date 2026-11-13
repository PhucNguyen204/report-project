# Media and report exports implementation plan

**Goal:** implement Y03/Y04 private image upload and UC22 asynchronous PDF/CSV exports.
**Architecture:** module-owned PostgreSQL tables, transactional authorization and River jobs; S3 calls outside database transactions. Stage uploads separately from immutable finalized objects.
**Spec:** chapters/c3/api.tex and backmatter/pl/f-dac-ta-api.tex; backend ADR01–08.

- [x] Media RED: real PostgreSQL/S3 upload, invalid content, IDOR, revoked role, finalized binding.
- [x] Media GREEN: schema041, upload/complete/validate callbacks, S3 private staging/final copy, HTTP routes.
- [x] Media check: focused PostgreSQL/S3 race tests, backend unit regression, vet.
- [x] Exports RED: atomic request/job, requester/member gate, CSV injection, Unicode PDF, retries and no partial publication.
- [x] Exports GREEN: schema042, report reader and S3 store ports, worker, routes, embedded OFL font.
- [x] Exports check: real S3 upload/download, PDF text/render inspection, focused race tests, backend unit regression, vet.
- [x] Lifecycle: scoped purge, reference-aware shared image retention, durable S3 deletion/replay, unpublished upload cleanup.
- [x] Periodic `Cleanup(ctx)`: expired uploads/staging, unbound images after30days, expired exports after7days.
- [ ] Root app/worker/scheduler composition and full backend integration/release gate.
- [ ] Root checkpoint/commit/push with shared progress report.

Review focus: signed staging overwrite must not alter finalized files; membership revocation must block complete/download; failed provider calls leave pending/failed rather than ready; retry/replay must not duplicate notifications; long Vietnamese report labels must wrap and paginate.

## Evidence 2026-11-13

- `go test -tags integration ./internal/modules/media ./internal/modules/exports -count=1 -race -coverprofile=../output/backend-qa/storage.cover -v`: media12 tests PASS, coverage85.8%; exports12 tests PASS, coverage85.1%; no skips. PostgreSQL localhost55432 and real S3-compatible SeaweedFS localhost59000. This is a local protocol smoke, not AWS production credential verification.
- `go test ./... -count=1 && go vet ./...`: PASS, exit0.
- PDF QA: embedded unmodified Noto Sans/OFL; `pdftotext`, `pdfinfo` and rendered first/last pages verified Vietnamese text, wrapping and pagination. Artifact: `output/backend-qa/exports/vietnamese-export.pdf` (ignored QA output).
- TDD behavioral RED observed for missing upload/complete behavior, missing HTTP routes, unsafe CSV/missing Unicode PDF, request+River rollback, scoped sharing, lifecycle deletion, unpublished S3 blob cleanup, period comparison, and retention cleanup. Malformed SQL in the retention fixture was corrected before observing its behavioral RED.

## Composition contract

- Media `New(db,*S3Store)`, `Register(mux)`, `Validate(ctx,tx,actor,purpose,target,objectID)`, `Read(ctx,user,id)`, `Handle(ctx,event)`, `PurgeSpace(ctx,tx,sid)`, `Cleanup(ctx)`.
- Exports `New(db,Reporter,Store)`, `Register(mux)`, `Request`, `Get`, `Process(ctx,exportID)`, `Handle(ctx,event)`, `PurgeSpace(ctx,tx,sid)`, `Cleanup(ctx)`.
- Dispatcher: media.delete/account.anonymized to media.Handle; report.export/export.delete to exports.Handle; notification to notification.Handle.
- Purge media and exports before planning/inventory/account deletion. Keep global idempotency on upload; Complete requires a key and uses natural object-ID replay outside the request-wide transaction so S3 I/O is outside DB transactions.
