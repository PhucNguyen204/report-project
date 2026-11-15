CREATE TABLE mealplan.ai_jobs (
 id uuid PRIMARY KEY,
 space_id uuid NOT NULL REFERENCES household.spaces(id) ON DELETE CASCADE,
 requested_by uuid NOT NULL REFERENCES identity.users(id) ON DELETE CASCADE,
 input jsonb NOT NULL CHECK(jsonb_typeof(input)='object'),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','ready','failed')),
 result jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(result)='array'),
 error_code text NOT NULL DEFAULT '' CHECK(error_code IN ('','provider_unavailable','provider_rejected','invalid_response','access_revoked','expired','invalid_request')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 2),
 attempt_token uuid,
 lease_until timestamptz,
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 completed_at timestamptz,
 CHECK((attempt_token IS NULL)=(lease_until IS NULL)),
 CHECK((status='pending')=(completed_at IS NULL))
);
CREATE INDEX ix_ai_jobs_requester ON mealplan.ai_jobs(space_id,requested_by,id);
CREATE INDEX ix_ai_jobs_expiry ON mealplan.ai_jobs(expires_at);
CREATE TABLE mealplan.ai_daily_quotas (
 user_id uuid NOT NULL REFERENCES identity.users(id) ON DELETE CASCADE,
 day date NOT NULL,
 accepted integer NOT NULL CHECK(accepted BETWEEN 1 AND 10),
 PRIMARY KEY(user_id,day)
);
