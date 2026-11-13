CREATE EXTENSION IF NOT EXISTS citext;
CREATE SCHEMA identity;
CREATE SCHEMA household;
CREATE SCHEMA catalog;
CREATE SCHEMA admin;
CREATE TABLE identity.users(
 id uuid PRIMARY KEY,email citext NOT NULL UNIQUE CHECK(char_length(email)<=100),full_name varchar(50) NOT NULL CHECK(char_length(full_name)>=2),password_hash text NOT NULL,
 system_role text NOT NULL DEFAULT 'user' CHECK(system_role IN ('user','admin')),status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','active','disabled')),
 token_version bigint NOT NULL DEFAULT 0,failed_login_count integer NOT NULL DEFAULT 0 CHECK(failed_login_count BETWEEN 0 AND 5),locked_until timestamptz,
 locale text NOT NULL DEFAULT 'vi' CHECK(locale IN ('vi','en')),avatar_object_id uuid,last_space_id uuid,last_login_at timestamptz,lock_reason text,
 deleted_at timestamptz,created_at timestamptz NOT NULL DEFAULT now(),version bigint NOT NULL DEFAULT 1
);
CREATE TABLE identity.otp_challenges(
 id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES identity.users(id),purpose text NOT NULL CHECK(purpose IN ('register','reset_password')),
 code_hash bytea NOT NULL,expires_at timestamptz NOT NULL,attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),used_at timestamptz,
 reset_token_hash bytea,reset_expires_at timestamptz,sent_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_otp_latest ON identity.otp_challenges(user_id,purpose,sent_at DESC);
CREATE TABLE identity.refresh_sessions(
 id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES identity.users(id),family_id uuid NOT NULL,token_hash bytea NOT NULL UNIQUE,device_id uuid NOT NULL,platform text NOT NULL CHECK(platform IN ('android','ios')),
 expires_at timestamptz NOT NULL,rotated_at timestamptz,revoked_at timestamptz,revoke_reason text,successor_id uuid,created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_refresh_family ON identity.refresh_sessions(family_id);
CREATE TABLE identity.user_settings(
 user_id uuid PRIMARY KEY REFERENCES identity.users(id),reminder_days integer CHECK(reminder_days BETWEEN 1 AND 7),expiry_notify_on boolean NOT NULL DEFAULT true,assignment_notify_on boolean NOT NULL DEFAULT true,activity_notify_on boolean NOT NULL DEFAULT true
);
CREATE TABLE identity.devices(
 id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES identity.users(id),fcm_token text NOT NULL UNIQUE,platform text NOT NULL CHECK(platform IN ('android','ios')),updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE household.spaces(
 id uuid PRIMARY KEY,kind text NOT NULL CHECK(kind IN ('personal','family')),name varchar(50) NOT NULL,change_seq bigint NOT NULL DEFAULT 0,created_by uuid NOT NULL REFERENCES identity.users(id),avatar_object_id uuid,dissolved_at timestamptz,created_at timestamptz NOT NULL DEFAULT now(),version bigint NOT NULL DEFAULT 1,UNIQUE(id,kind)
);
CREATE TABLE household.space_members(
 space_id uuid NOT NULL,user_id uuid NOT NULL REFERENCES identity.users(id),role text NOT NULL CHECK(role IN ('leader','member')),space_kind text NOT NULL,joined_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(space_id,user_id),FOREIGN KEY(space_id,space_kind) REFERENCES household.spaces(id,kind)
);
CREATE UNIQUE INDEX uq_member_family ON household.space_members(user_id) WHERE space_kind='family';
CREATE UNIQUE INDEX uq_member_personal ON household.space_members(user_id) WHERE space_kind='personal';
CREATE UNIQUE INDEX uq_space_leader ON household.space_members(space_id) WHERE role='leader';
CREATE TABLE household.invite_codes(id uuid PRIMARY KEY,space_id uuid NOT NULL REFERENCES household.spaces(id),code text NOT NULL UNIQUE,created_by uuid NOT NULL REFERENCES identity.users(id),expires_at timestamptz NOT NULL,revoked_at timestamptz,created_at timestamptz NOT NULL DEFAULT now());
CREATE UNIQUE INDEX uq_active_invite ON household.invite_codes(space_id) WHERE revoked_at IS NULL;
CREATE TABLE catalog.food_categories(id uuid PRIMARY KEY,name varchar(100) NOT NULL,active boolean NOT NULL DEFAULT true);
CREATE UNIQUE INDEX uq_category_name ON catalog.food_categories(lower(name));
CREATE TABLE catalog.units(id uuid PRIMARY KEY,name varchar(50) NOT NULL,dimension text NOT NULL CHECK(dimension IN ('mass','volume','count')),factor_to_base numeric(12,4) NOT NULL CHECK(factor_to_base>0),is_base boolean NOT NULL DEFAULT false,active boolean NOT NULL DEFAULT true,CHECK(NOT is_base OR factor_to_base=1));
CREATE UNIQUE INDEX uq_unit_name ON catalog.units(lower(name));
CREATE UNIQUE INDEX uq_base_unit ON catalog.units(dimension) WHERE is_base;
CREATE TABLE catalog.foods(id uuid PRIMARY KEY,name varchar(100) NOT NULL,category_id uuid NOT NULL REFERENCES catalog.food_categories(id),scope text NOT NULL DEFAULT 'global' CHECK(scope IN ('global','space')),owner_space_id uuid REFERENCES household.spaces(id),default_unit_id uuid NOT NULL REFERENCES catalog.units(id),status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','pending_review','inactive')),default_location text NOT NULL DEFAULT 'chill',merged_into uuid REFERENCES catalog.foods(id),version bigint NOT NULL DEFAULT 1,CHECK((scope='global')=(owner_space_id IS NULL)));
CREATE UNIQUE INDEX uq_global_food ON catalog.foods(category_id,lower(name)) WHERE scope='global';
CREATE UNIQUE INDEX uq_space_food ON catalog.foods(owner_space_id,category_id,lower(name)) WHERE scope='space';
CREATE TABLE catalog.food_shelf_lives(food_id uuid NOT NULL REFERENCES catalog.foods(id),storage_location text NOT NULL,days integer NOT NULL CHECK(days>0),PRIMARY KEY(food_id,storage_location));
CREATE TABLE admin.system_configs(key text PRIMARY KEY,value jsonb NOT NULL,updated_by uuid REFERENCES identity.users(id),updated_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE admin.audit_logs(id uuid PRIMARY KEY,actor_id uuid NOT NULL REFERENCES identity.users(id),action text NOT NULL,entity_id text,old_value jsonb,new_value jsonb,created_at timestamptz NOT NULL DEFAULT now());
INSERT INTO admin.system_configs(key,value) VALUES
 ('reminder.defaultDays','3'),('reminder.runAt','"08:00"'),('reminder.maxLotsPerNotice','5'),('group.maxMembers','10'),('invite.ttlHours','48'),('otp.ttlMinutes','5'),('suggest.theta','0.6'),('suggest.weights','{"r":0.6,"e":0.3,"f":0.1}'),('suggest.maxResults','10'),('mealplan.horizonDays','28');
INSERT INTO catalog.food_categories(id,name) VALUES('00000000-0000-7000-8000-000000000001','Rau củ'),('00000000-0000-7000-8000-000000000002','Thực phẩm khác');
INSERT INTO catalog.units(id,name,dimension,factor_to_base,is_base) VALUES
 ('00000000-0000-7000-8000-000000000011','g','mass',1,true),('00000000-0000-7000-8000-000000000012','kg','mass',1000,false),('00000000-0000-7000-8000-000000000013','lạng','mass',100,false),
 ('00000000-0000-7000-8000-000000000014','ml','volume',1,true),('00000000-0000-7000-8000-000000000015','l','volume',1000,false),('00000000-0000-7000-8000-000000000016','cái','count',1,true);
INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES('00000000-0000-7000-8000-000000000021','Cà rốt','00000000-0000-7000-8000-000000000001','00000000-0000-7000-8000-000000000011');
INSERT INTO catalog.food_shelf_lives(food_id,storage_location,days) VALUES('00000000-0000-7000-8000-000000000021','chill',7),('00000000-0000-7000-8000-000000000021','freezer',30);
