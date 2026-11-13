CREATE TABLE household.join_attempts(user_id uuid PRIMARY KEY REFERENCES identity.users(id),window_start timestamptz NOT NULL,hits integer NOT NULL);
