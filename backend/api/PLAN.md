# Backend OpenAPI contract implementation plan

> Execution follows the backend plan with TDD and root-owned Git operations.

Goal: publish a complete OpenAPI 3.1 contract for the running backend and verify representative real HTTP payloads against its schemas.

Architecture: `api/openapi.json` is the source artifact, embedded by `api/document.go`. Standard JSON is also valid YAML. Tests compile Draft 2020-12 schemas, validate the document against the pinned official OpenAPI schema, resolve all references, compare documented operations with the registered ServeMux, and validate real HTTP requests/responses. No runtime validator is installed in request flows.

Spec: `chapters/c3/api.tex`, `backmatter/pl/f-dac-ta-api.tex`, actual handlers and DTOs under `backend/internal/modules`.

Constraints: OpenAPI 3.1; Vietnamese examples; exact fixed-point quantities; real route/DTO names; required application/idempotency/version headers; Bearer authentication; Y01/Y02 deferred and P08 removed. Root owns dependencies and Git.

Review focus: optional/null fields; union responses such as OTP/export/generation; tenant-scoped route bindings; monetary/quantity precision; header exceptions for public authentication, password changes and media finalization.

- [x] Read registrations, DTOs, SQL-backed response columns and primary API mappings.
- [x] Add failing document/ref/route/schema behavior tests.
- [x] Build explicit reusable request/response schemas and all actual operations.
- [x] Compile/validate official OAS structure and every local reference without network in tests.
- [x] Validate representative real HTTP scenarios and mutations that remove required fields or change data types.
- [x] Run unit/integration contract tests, vet and diff checks; hand root exact artifact/test commit group and verification evidence.

Root verification13/11/2026: full integration/race suite PASS; UUID header regression observed RED then GREEN. 74 paths/101 operations/198 schemas. Pagination gaps are tracked in the shared progress report and are not waived by schema validation.
