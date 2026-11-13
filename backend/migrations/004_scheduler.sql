CREATE TABLE platform.scheduler_runs(slot timestamptz PRIMARY KEY,created_at timestamptz NOT NULL DEFAULT now());
