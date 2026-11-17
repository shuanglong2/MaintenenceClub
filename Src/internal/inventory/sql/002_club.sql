ALTER TABLE inventory.items DROP CONSTRAINT inventory_item_category_check;

UPDATE inventory.items SET category = '仪器' WHERE category = '器材' AND tracked;
UPDATE inventory.items SET category = '设备' WHERE category = '器材';
UPDATE inventory.items SET category = '耗材' WHERE category = '消耗品';
UPDATE inventory.items SET category = '零件' WHERE category = '元件';

ALTER TABLE inventory.items ADD CONSTRAINT inventory_item_category_check CHECK (category IN (
    '器材', '消耗品', '工具', '元件', '设备', '仪器', '零件', '耗材', '日用品'
));

ALTER TABLE inventory.items
    ADD COLUMN purpose text NOT NULL DEFAULT '',
    ADD COLUMN model text NOT NULL DEFAULT '',
    ADD COLUMN value_text text NOT NULL DEFAULT '',
    ADD COLUMN received_at timestamptz,
    ADD COLUMN last_used_at timestamptz,
    ADD COLUMN condition text NOT NULL DEFAULT 'ok',
    ADD CONSTRAINT inventory_purpose_check CHECK (char_length(purpose) <= 80),
    ADD CONSTRAINT inventory_model_check CHECK (char_length(model) <= 80),
    ADD CONSTRAINT inventory_value_check CHECK (char_length(value_text) <= 40),
    ADD CONSTRAINT inventory_condition_check CHECK (condition IN ('ok', 'repair', 'replace'));

UPDATE inventory.items SET received_at = created_at WHERE received_at IS NULL;
