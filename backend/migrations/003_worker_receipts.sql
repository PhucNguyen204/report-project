CREATE TABLE platform.worker_receipts (
 event_key text PRIMARY KEY CHECK(length(event_key)>0),
 kind text NOT NULL CHECK(length(kind)>0),
 created_at timestamptz NOT NULL DEFAULT now()
);
