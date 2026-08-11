ALTER TABLE android_system_image_catalog
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_api,
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_type,
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_abi,
    DROP CONSTRAINT IF EXISTS ck_android_system_image_catalog_package;
ALTER TABLE android_system_image_catalog
    ADD CONSTRAINT ck_android_system_image_catalog_api CHECK (api_level BETWEEN 33 AND 36),
    ADD CONSTRAINT ck_android_system_image_catalog_type CHECK (image_type IN ('google_apis', 'google_play')),
    ADD CONSTRAINT ck_android_system_image_catalog_abi CHECK (abi = 'x86_64'),
    ADD CONSTRAINT ck_android_system_image_catalog_package
        CHECK (package_name ~ '^system-images;android-(33|34|35|36);(google_apis|google_play);x86_64$');
