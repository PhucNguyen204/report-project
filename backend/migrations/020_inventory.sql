CREATE SCHEMA IF NOT EXISTS pantry;
CREATE SCHEMA IF NOT EXISTS shopping;

CREATE TABLE pantry.food_lots (
 id uuid PRIMARY KEY,
 space_id uuid NOT NULL REFERENCES household.spaces(id),
 food_id uuid NOT NULL REFERENCES catalog.foods(id),
 unit_id uuid NOT NULL REFERENCES catalog.units(id),
 unit_factor numeric(12,4) NOT NULL CHECK(unit_factor>0),
 dimension text NOT NULL,
 initial_qty numeric(12,2) NOT NULL CHECK(initial_qty>0),
 remaining_qty numeric(12,2) NOT NULL CHECK(remaining_qty>=0 AND remaining_qty<=initial_qty),
 base_remaining numeric(20,6) NOT NULL CHECK(base_remaining>=0),
 purchased_on date NOT NULL,
 expires_on date NOT NULL CHECK(expires_on>=purchased_on),
 storage_location text NOT NULL CHECK(storage_location IN ('chill','freezer','crisper','door','pantry_shelf')),
 total_cost bigint CHECK(total_cost>=0),
 source_item_id uuid UNIQUE,
 lifecycle text NOT NULL DEFAULT 'active' CHECK(lifecycle IN ('active','consumed','discarded')),
 version bigint NOT NULL DEFAULT 1,
 notes text NOT NULL DEFAULT '' CHECK(char_length(notes)<=200),
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((lifecycle='active')=(remaining_qty>0))
);
CREATE INDEX ix_lots_fefo ON pantry.food_lots(space_id,food_id,expires_on,id) WHERE lifecycle='active' AND deleted_at IS NULL;
CREATE INDEX ix_lots_expiry ON pantry.food_lots(space_id,expires_on) WHERE lifecycle='active' AND deleted_at IS NULL;

CREATE TABLE pantry.stock_movements (
 id uuid PRIMARY KEY,
 space_id uuid NOT NULL REFERENCES household.spaces(id),
 lot_id uuid NOT NULL REFERENCES pantry.food_lots(id),
 food_id uuid NOT NULL REFERENCES catalog.foods(id),
 kind text NOT NULL CHECK(kind IN ('stock_in','consume','discard','adjust')),
 quantity numeric(20,6) NOT NULL,
 reason text NOT NULL DEFAULT '',
 meal_id uuid,
 actor_id uuid,
 occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_movements_space_time ON pantry.stock_movements(space_id,occurred_at);
CREATE UNIQUE INDEX uq_movement_meal_lot ON pantry.stock_movements(meal_id,lot_id) WHERE meal_id IS NOT NULL AND kind='consume';

CREATE TABLE shopping.shopping_lists (
 id uuid PRIMARY KEY,
 space_id uuid NOT NULL REFERENCES household.spaces(id),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100),
 planned_date date NOT NULL,
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','shopping','completed')),
 notes text NOT NULL DEFAULT '' CHECK(char_length(notes)<=200),
 version bigint NOT NULL DEFAULT 1,
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_lists_space_date ON shopping.shopping_lists(space_id,planned_date,id) WHERE deleted_at IS NULL;

CREATE TABLE shopping.shopping_items (
 id uuid PRIMARY KEY,
 space_id uuid NOT NULL REFERENCES household.spaces(id),
 list_id uuid NOT NULL REFERENCES shopping.shopping_lists(id),
 food_id uuid REFERENCES catalog.foods(id),
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 100),
 unit_id uuid NOT NULL REFERENCES catalog.units(id),
 unit_factor numeric(12,4) NOT NULL CHECK(unit_factor>0),
 dimension text NOT NULL,
 quantity numeric(12,2) NOT NULL CHECK(quantity>0),
 actual_quantity numeric(12,2) CHECK(actual_quantity>0),
 unit_price bigint CHECK(unit_price>=0),
 status text NOT NULL DEFAULT 'to_buy' CHECK(status IN ('to_buy','assigned','purchased','skipped','stocked')),
 assignee_id uuid,
 purchased_by uuid,
 purchased_at timestamptz,
 lot_id uuid,
 notes text NOT NULL DEFAULT '' CHECK(char_length(notes)<=200),
 version bigint NOT NULL DEFAULT 1,
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_items_list ON shopping.shopping_items(space_id,list_id,id) WHERE deleted_at IS NULL;
CREATE INDEX ix_items_pending ON shopping.shopping_items(space_id,food_id) WHERE status IN ('to_buy','assigned','purchased') AND deleted_at IS NULL;
