CREATE TABLE report.exports (
 id uuid PRIMARY KEY,
 space_id uuid NOT NULL REFERENCES household.spaces(id),
 requested_by uuid NOT NULL REFERENCES identity.users(id),
 report_type text NOT NULL CHECK(report_type IN ('spending','consumption','waste')),
 from_date date NOT NULL,
 to_date date NOT NULL CHECK(to_date>=from_date AND to_date<=from_date+interval '12 months'),
 format text NOT NULL CHECK(format IN ('pdf','csv')),
 group_by text NOT NULL CHECK(group_by IN ('day','week','month')),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','ready','failed')),
 object_key text UNIQUE,
 error text NOT NULL DEFAULT '',
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz,
 CHECK((status='ready')=(object_key IS NOT NULL AND completed_at IS NOT NULL))
);
CREATE INDEX ix_exports_requester ON report.exports(space_id,requested_by,id);
