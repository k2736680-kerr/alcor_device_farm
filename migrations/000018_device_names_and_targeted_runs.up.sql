ALTER TABLE devices
    ADD COLUMN name varchar(40) NOT NULL DEFAULT '未命名设备';

UPDATE devices
SET name = left(
    CASE
        WHEN platform = 'ios' THEN COALESCE(NULLIF(capabilities->>'model', ''), 'iOS设备')
        ELSE COALESCE(NULLIF(capabilities->>'hardware_profile_name', ''), 'Android设备')
    END || '-' || right(id, 6),
    40
);

ALTER TABLE devices
    ADD CONSTRAINT ck_devices_name CHECK (char_length(btrim(name)) BETWEEN 2 AND 40);
