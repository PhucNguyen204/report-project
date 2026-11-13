# Notification completion implementation plan

**Goal:** complete authenticated inbox pagination, provider delivery safety, and retention with real PostgreSQL/SMTP evidence and module coverage >=85%.
**Architecture:** notification owns inbox/delivery writes; provider I/O stays outside transactions. Keep `PushSender.Send` compatible; optional structured push messages carry safe navigation metadata.
**Tech Stack:** Go, pgx/PostgreSQL, net/smtp, FCM HTTP v1, real local Mailpit.
**Spec:** chapters/c3/api.tex A17/A18 and pagination; chapters/c3/csdl.tex notification90days/delivery30days retention; parent backend plan and AGENTS.md.

Global constraints: only notification/** changes; retain root PurgeUser; do not edit040; no Git/shared report changes; typed400 malformed input without payload or provider secrets.

Review focus: equal-time cursor ties and concurrent insert; foreign mark-read IDs; user/settings revoked before retry; FCM diagnostic text mistaken for invalid token; SMTP cancellation/header injection.

- [ ] Inbox RED: assert default20/max100, stable cursor pages/invalid cursor, own-only read and mark.
- [ ] Inbox GREEN: `List(ctx,user,limit,cursor)` + camelCase item/page DTO, keyset(created_at DESC,id ASC), meta.nextCursor, authenticated register handlers.
- [ ] Worker/provider RED: malformed events typed400, disabled/inactive suppression, missing provider, invalid token/per-device retry with persisted outcomes, safe metadata HTTP contract.
- [ ] Worker/provider GREEN: validation, structured FCM adapter, safe status classification and redacted persisted errors; retain successful delivery replay.
- [ ] SMTP RED/GREEN: real Mailpit SMTP delivery read through HTTP API, MIME Unicode/headers, prompt context cancellation and TLS-required failure.
- [ ] Retention RED/GREEN: `Cleanup(ctx) error`, transactionally remove inbox older90days and deliveries older30days; preserve current records and PurgeUser behavior.
- [ ] Focused real PG+Mailpit race tests/coverage, full backend unit suite, vet; freeze component for root commit/push.
