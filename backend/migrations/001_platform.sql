CREATE SCHEMA IF NOT EXISTS platform;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE TABLE platform.activities (
  space_id uuid NOT NULL, seq bigint NOT NULL, actor_id uuid NOT NULL,
  kind text NOT NULL, entity_id text NOT NULL, payload jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(space_id, seq)
);
CREATE TABLE platform.idempotency_keys (
  user_id uuid NOT NULL, key text NOT NULL, request_hash text NOT NULL,
  status integer, response bytea, headers jsonb,
  created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,key)
);
