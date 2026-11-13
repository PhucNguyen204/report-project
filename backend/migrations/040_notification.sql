CREATE SCHEMA notification;
CREATE TABLE notification.notifications(
 id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES identity.users(id),dedupe_key text NOT NULL UNIQUE,
 title text NOT NULL,body text NOT NULL,type text NOT NULL,read_at timestamptz,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_notification_user ON notification.notifications(user_id,created_at DESC,id);
CREATE TABLE notification.deliveries(
 notification_id uuid NOT NULL REFERENCES notification.notifications(id),device_id uuid NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sent','invalid')),
 attempts integer NOT NULL DEFAULT 0,last_error text,sent_at timestamptz,PRIMARY KEY(notification_id,device_id)
);
