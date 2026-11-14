# Actor and session revocation implementation plan

> **For agentic workers:** TDD +verification-before-completion. Test agent sở hữu actor_revocation_integration_test.go; root sở hữu production và Git; reviewer độc lập chỉ đọc.

**Goal:** mutation không dùng quyền/phiên đã bị thu hồi trong cửa sổ sau Authenticate, kể cả admin promotion/disable/delete/logout/token-version change.

**Architecture:** DB.LockActor giữ active identity FOR SHARE và current refresh-family FOR SHARE khi context chứa authenticated Principal của chính actor; kiểm current role/token_version. LockMember dùng guard trước member lock; admin/template/media tái dùng cùng guard. RequireMember preflight giữ semantics không khóa để không đặt identity lock trước group advisory hiện có. Refresh đọc immutable owner của token, khóa identity UPDATE, rồi đọc lại token/session UPDATE; giữ grace/reuse/outcome semantics.

**Tech Stack:** Go1.26.11, PostgreSQL16/pgx, existing JWT/session families.

**Spec:** BR23/UC02/UC24, backend plan reviewfocus2; independent real-router/JWT/PG repro14/11 cho3cases đã201/1row sau revoke. Không đổi scope LaTeX.

## Global constraints

- Linearization: revocation commit trước authorization guard→deny; mutation đã giữ guard→revoker phải chờ mutation commit. Không hứa hủy mọi request đã authorize.
- Lock order: existing group advisory (nếu cần)→identity actor→current refresh session→member→target; admin_roles trước admin actor. Không giữ session UPDATE rồi lấy identity lock trong refresh.
- Principal.TokenVersion là context nội bộ, không input/client DTO. Chỉ kiểm family actor khi Principal.UserID khớp actor và SessionID có giá trị; recipient/background vẫn kiểm identity hiện tại.
- Không sửa migration/schema, không provider I/O trong transaction, không nới TTL/test timeout để qua gate.
- RequireMember preflight không giữ actor/session lock qua group advisory; mutation phải LockMember/LockActor trong transaction thích hợp.

## Review focus

1. Stale principal role user nhưng DB đãadmin/disabled/deleted phải không tạo resource/activity/idempotency row.
2. Logout chỉ revoke refresh-family, giữ user/version, vẫn phải chặn trước mutation; version-only change cũng bị chặn.
3. Mutation guard trước revoke giữ thứ tự commit; logout/revoke không hoàn tất khi mutation đang chờ target row.
4. Refresh/reuse/grace/password reset/admin updates không đảo identity/session order hoặc làm outcome counter rollback.
5. Group create/join không gọi LockMember nên cần guard sau existing identity UPDATE; assignment/transfer recipient không dùng nhầm family actor.

### Task1: shared actor/session guard and refresh ordering

Files: platform/{contracts.go,db.go,actor.go,idempotency.go}; accounts/{service.go,groups.go,admin_authorization.go,profile.go,privacy.go}; planning/{templates.go,http_extra.go,recipes.go}; media/service.go; notification/inbox.go; app actor/favorite tests, accounts refresh/logout lock-order tests.

Interfaces: DB.LockActor(ctx,tx,actorID)(currentRole,error), DB.CheckActor(ctx,q,actorID)(currentRole,error); Principal thêm TokenVersion int64 do Authenticate cấp. Missing/inactive actor từ non-auth service context vẫn được caller map404/403; authenticated stale-session lỗi401. Read preflight không khóa nhưng kiểm identity/session; LockMember giữ actor/family đếncommit.

- [x] Actual RED5stale-auth cases và3group create/join cases; second-order serialization test không ép cả2thao tác phải abort.19leaf cases/22testrecords GREEN,7route có positivecontrol freshlogin.
- [x] Shared guard, capture version, refresh actor-first/re-read session; reuse admin/template/media/own-account/notification guards. Logout giữidentity UPDATE trước familyguard để tránh share→update cycle.
- [x] Targeted concurrency/password/session/grace/admin/template/media regressions, rồi full lint/unit/integration/race/security PASS548Go/0fail/0skip +7Python; global85.72%, mọi package/core gate đạt. Review độc lập tìmfinding, root kiểm fix/regressions; finalwhole-branch rereview vẫn mở nếu reviewer quota hết.
- [x] Report rõwallclock-interrupted run/pre-fixLinuxproof vsfinaltree; stagedscan vàGit thực hiện ngay sau gate.

Additional review: SetFavorite thiếuguard chologout và choadmincònmembership.2actualRED HTTP200/privaterelation vàlogoutkhôngđợi→LockActor→nonlocking scope discovery→LockMember cho space→recipeFORSHARE recheck→GREEN. Globalpublishedtemplate vẫn dùng được theo ownfavorite API. Refresh lock-order regression GREENcurrent; isolatedrestore ONLYoldrefresh RED55P03 khi đọccurrent-sessionSHARE NOWAIT. Logoutidentity-first test REDlogoutHTTP200trướckhi actorXlock→GREEN.

Fullfirst-run14/11:526pass/22fail (gồmparenttestrecords). Time-related failures cówindow11:42–13:52 nhưngmaxpackage165.9s; giữartifact actor-regression-first-failure-20261114.jsonl. Lỗi fixture thật được sửa:3middlewaretests dùng principal chưa cóidentityrow, fixture foreignrecipe dùngadmin vi phạmBR23, lock-wait detector quákhớpSELECTcolumns. Không nới TTL/timeout hoặc bỏtest. Fullstablewindow13:58:09–13:59:55→548pass.
