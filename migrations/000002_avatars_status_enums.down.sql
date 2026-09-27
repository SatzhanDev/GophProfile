ALTER TABLE avatars
    ALTER COLUMN upload_status DROP DEFAULT,
    ALTER COLUMN upload_status TYPE VARCHAR(50) USING upload_status::text,
    ALTER COLUMN upload_status SET DEFAULT 'uploading';

ALTER TABLE avatars
    ALTER COLUMN processing_status DROP DEFAULT,
    ALTER COLUMN processing_status TYPE VARCHAR(50) USING processing_status::text,
    ALTER COLUMN processing_status SET DEFAULT 'pending';

DROP TYPE IF EXISTS upload_status_type;
DROP TYPE IF EXISTS processing_status_type;
