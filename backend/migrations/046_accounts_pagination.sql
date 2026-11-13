CREATE INDEX ix_admin_users_created ON identity.users(created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX ix_admin_audit_created ON admin.audit_logs(created_at DESC,id DESC);
CREATE INDEX ix_admin_foods_name ON catalog.foods(name,id);
CREATE INDEX ix_admin_categories_name ON catalog.food_categories(name,id);
CREATE INDEX ix_admin_units_name ON catalog.units(name,id);
