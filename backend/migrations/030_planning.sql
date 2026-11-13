CREATE SCHEMA IF NOT EXISTS recipe;
CREATE SCHEMA IF NOT EXISTS mealplan;
CREATE SCHEMA IF NOT EXISTS report;

CREATE TABLE recipe.recipes (
 id uuid PRIMARY KEY,
 space_id uuid REFERENCES household.spaces(id),
 created_by uuid NOT NULL REFERENCES identity.users(id),
 name text NOT NULL CHECK(char_length(name) BETWEEN 2 AND 100),
 servings integer NOT NULL CHECK(servings BETWEEN 1 AND 20),
 kind text NOT NULL DEFAULT 'space' CHECK(kind IN ('space','template')),
 status text NOT NULL DEFAULT 'published' CHECK(status IN ('draft','published','hidden')),
 meal_slot text NOT NULL DEFAULT '' CHECK(meal_slot IN ('','breakfast','lunch','dinner')),
 cook_minutes integer NOT NULL DEFAULT 0 CHECK(cook_minutes BETWEEN 0 AND 1440),
 steps jsonb NOT NULL DEFAULT '[]', notes text NOT NULL DEFAULT '',
 origin_recipe_id uuid,
 version bigint NOT NULL DEFAULT 1,
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK((kind='template')=(space_id IS NULL)),
 CHECK(kind='template' OR status='published')
);
CREATE INDEX ix_recipes_space ON recipe.recipes(space_id,id) WHERE deleted_at IS NULL;
CREATE TABLE recipe.recipe_ingredients (
 id uuid PRIMARY KEY,
 recipe_id uuid NOT NULL REFERENCES recipe.recipes(id),
 food_id uuid NOT NULL REFERENCES catalog.foods(id),
 unit_id uuid NOT NULL REFERENCES catalog.units(id),
 unit_factor numeric(12,4) NOT NULL CHECK(unit_factor>0),
 dimension text NOT NULL,
 quantity numeric(12,2) NOT NULL CHECK(quantity>0),
 optional boolean NOT NULL DEFAULT false,
 UNIQUE(recipe_id,food_id)
);
CREATE TABLE recipe.favorite_recipes (
 user_id uuid NOT NULL REFERENCES identity.users(id),
 recipe_id uuid NOT NULL REFERENCES recipe.recipes(id),
 PRIMARY KEY(user_id,recipe_id)
);
CREATE TABLE mealplan.meal_entries (
 id uuid PRIMARY KEY,
 space_id uuid NOT NULL REFERENCES household.spaces(id),
 recipe_id uuid,
 name text NOT NULL CHECK(char_length(name) BETWEEN 2 AND 100),
 meal_date date NOT NULL,
 meal_slot text NOT NULL CHECK(meal_slot IN ('breakfast','lunch','dinner')),
 servings integer NOT NULL CHECK(servings BETWEEN 1 AND 20),
 status text NOT NULL DEFAULT 'planned' CHECK(status IN ('planned','cooked')),
 cooked_at timestamptz,
 actual_servings integer CHECK(actual_servings BETWEEN 1 AND 20),
 skipped_stock boolean NOT NULL DEFAULT false,
 created_by uuid NOT NULL REFERENCES identity.users(id),
 version bigint NOT NULL DEFAULT 1,
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ix_meals_space_date ON mealplan.meal_entries(space_id,meal_date,id) WHERE deleted_at IS NULL;

CREATE VIEW report.v_spending AS
 SELECT i.space_id,i.id,i.food_id,i.name,i.purchased_at,
        i.actual_quantity,i.unit_price,i.unit_factor,i.dimension,
        f.category_id,(SELECT c.name FROM catalog.food_categories c WHERE c.id=f.category_id) AS category_name
 FROM shopping.shopping_items i
 LEFT JOIN catalog.foods f ON f.id=i.food_id
 WHERE i.status IN ('purchased','stocked') AND i.deleted_at IS NULL;
CREATE VIEW report.v_movements AS
 SELECT m.id,m.space_id,m.lot_id,m.food_id,m.kind,m.quantity,m.reason,m.meal_id,m.occurred_at,
        l.initial_qty,l.unit_factor,l.dimension,l.total_cost,f.name,f.category_id,(SELECT c.name FROM catalog.food_categories c WHERE c.id=f.category_id) AS category_name
 FROM pantry.stock_movements m
 JOIN pantry.food_lots l ON l.id=m.lot_id AND l.space_id=m.space_id
 JOIN catalog.foods f ON f.id=m.food_id;
