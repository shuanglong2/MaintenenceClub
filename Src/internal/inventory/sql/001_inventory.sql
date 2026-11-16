CREATE SCHEMA inventory;

CREATE TABLE inventory.locations (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    parent_id bigint REFERENCES inventory.locations (id),
    name text NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    CONSTRAINT inventory_location_name_check CHECK (char_length(name) BETWEEN 1 AND 40)
);

CREATE TABLE inventory.items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code text NOT NULL,
    name text NOT NULL,
    category text NOT NULL,
    spec text NOT NULL DEFAULT '',
    unit text NOT NULL DEFAULT '个',
    location_id bigint REFERENCES inventory.locations (id),
    min_qty integer NOT NULL DEFAULT 0,
    tracked boolean NOT NULL DEFAULT false,
    photo_id bigint REFERENCES files.files (id),
    note text NOT NULL DEFAULT '',
    voided_at timestamptz,
    void_reason text NOT NULL DEFAULT '',
    voided_by bigint REFERENCES people.people (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_item_code_unique UNIQUE (code),
    CONSTRAINT inventory_item_code_check CHECK (char_length(code) BETWEEN 1 AND 32),
    CONSTRAINT inventory_item_name_check CHECK (char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT inventory_item_category_check CHECK (category IN ('器材', '消耗品', '工具', '元件')),
    CONSTRAINT inventory_item_min_check CHECK (min_qty >= 0)
);

CREATE INDEX inventory_items_category ON inventory.items (category);
CREATE INDEX inventory_items_location ON inventory.items (location_id);

CREATE TABLE inventory.movements (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id bigint NOT NULL REFERENCES inventory.items (id),
    kind text NOT NULL,
    qty integer NOT NULL,
    actor_id bigint REFERENCES people.people (id),
    actor_name text NOT NULL DEFAULT '',
    note text NOT NULL DEFAULT '',
    ref_type text NOT NULL DEFAULT '',
    ref_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_movement_kind_check CHECK (kind IN ('in', 'use', 'adjust', 'scrap')),
    CONSTRAINT inventory_movement_qty_check CHECK (qty <> 0)
);

CREATE INDEX inventory_movements_item ON inventory.movements (item_id);

CREATE TABLE inventory.equipment_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    item_id bigint NOT NULL REFERENCES inventory.items (id),
    kind text NOT NULL,
    actor_id bigint REFERENCES people.people (id),
    actor_name text NOT NULL DEFAULT '',
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT inventory_event_kind_check CHECK (kind IN ('borrow', 'return', 'repair', 'repaired', 'scrap'))
);

CREATE INDEX inventory_events_item ON inventory.equipment_events (item_id, id DESC);

CREATE TABLE inventory.stocktakes (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title text NOT NULL,
    status text NOT NULL DEFAULT 'open',
    created_by bigint REFERENCES people.people (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    confirmed_at timestamptz,
    confirmed_by bigint REFERENCES people.people (id),
    CONSTRAINT inventory_stocktake_status_check CHECK (status IN ('open', 'confirmed')),
    CONSTRAINT inventory_stocktake_title_check CHECK (char_length(title) BETWEEN 1 AND 40)
);

CREATE TABLE inventory.stocktake_lines (
    stocktake_id bigint NOT NULL REFERENCES inventory.stocktakes (id),
    item_id bigint NOT NULL REFERENCES inventory.items (id),
    expected_qty integer NOT NULL,
    actual_qty integer,
    PRIMARY KEY (stocktake_id, item_id),
    CONSTRAINT inventory_stocktake_actual_check CHECK (actual_qty IS NULL OR actual_qty >= 0)
);

CREATE FUNCTION inventory.reject_change() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '库存流水不能修改或删除';
END;
$$;

CREATE TRIGGER inventory_movements_no_update
BEFORE UPDATE ON inventory.movements
FOR EACH ROW EXECUTE FUNCTION inventory.reject_change();

CREATE TRIGGER inventory_movements_no_delete
BEFORE DELETE ON inventory.movements
FOR EACH ROW EXECUTE FUNCTION inventory.reject_change();

CREATE TRIGGER inventory_events_no_update
BEFORE UPDATE ON inventory.equipment_events
FOR EACH ROW EXECUTE FUNCTION inventory.reject_change();

CREATE TRIGGER inventory_events_no_delete
BEFORE DELETE ON inventory.equipment_events
FOR EACH ROW EXECUTE FUNCTION inventory.reject_change();
