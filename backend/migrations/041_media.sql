CREATE SCHEMA media;
CREATE TABLE media.objects (
 id uuid PRIMARY KEY,
 uploaded_by uuid NOT NULL REFERENCES identity.users(id),
 purpose text NOT NULL CHECK(purpose IN ('avatar','space','recipe','food')),
 target_id uuid NOT NULL,
 content_type text NOT NULL CHECK(content_type IN ('image/png','image/jpeg')),
 size bigint NOT NULL CHECK(size>0 AND size<=5242880),
 staging_key text NOT NULL UNIQUE,
 object_key text UNIQUE,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','ready')),
 expires_at timestamptz NOT NULL,
 completed_at timestamptz,
 staging_cleanup_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((status='ready')=(object_key IS NOT NULL AND completed_at IS NOT NULL))
);
CREATE INDEX ix_media_owner ON media.objects(uploaded_by,id);
