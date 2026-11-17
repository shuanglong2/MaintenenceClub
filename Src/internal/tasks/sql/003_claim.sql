ALTER TABLE tasks.tasks DROP CONSTRAINT tasks_kind_check;

ALTER TABLE tasks.tasks ADD CONSTRAINT tasks_kind_check CHECK (kind IN (
    '科普活动', '创新竞赛', '实验室保养', '电脑软硬件维护', '其他',
    '小家用电器维修', '实验室仪器保养', '电子设备类维修', '维修工具外借出'
));

ALTER TABLE tasks.tasks
    ADD COLUMN contact text NOT NULL DEFAULT '',
    ADD COLUMN claimed_by bigint REFERENCES people.people (id),
    ADD COLUMN claimed_name text NOT NULL DEFAULT '',
    ADD COLUMN claimed_at timestamptz,
    ADD CONSTRAINT tasks_contact_check CHECK (char_length(contact) <= 80);
