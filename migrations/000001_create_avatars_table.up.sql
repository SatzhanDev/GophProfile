-- pgcrypto нужен для gen_random_uuid(); на PostgreSQL 13+ функция есть и без
-- расширения, но включаем явно для совместимости с более старыми версиями.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ENUM вместо VARCHAR(50): набор значений и так уже закрытый и описан
-- константами в Go (domain.UploadStatus/domain.ProcessingStatus) — ENUM
-- переносит эту же гарантию на уровень БД, не давая записать невалидное
-- значение мимо приложения (например, при ручном UPDATE или из другого сервиса).
CREATE TYPE upload_status_type AS ENUM ('uploading', 'completed', 'failed');
CREATE TYPE processing_status_type AS ENUM ('pending', 'processing', 'completed', 'failed');

CREATE TABLE IF NOT EXISTS avatars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    s3_key VARCHAR(500) NOT NULL,
    thumbnail_s3_keys JSONB NULL,
    upload_status upload_status_type NOT NULL DEFAULT 'uploading',
    processing_status processing_status_type NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_avatars_user_id ON avatars(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_avatars_status ON avatars(upload_status, processing_status);
