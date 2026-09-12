-- ENUM вместо VARCHAR(50): набор значений и так уже закрытый и описан
-- константами в Go (domain.UploadStatus/domain.ProcessingStatus) — ENUM
-- переносит эту же гарантию на уровень БД, не давая записать невалидное
-- значение мимо приложения (например, при ручном UPDATE или из другого сервиса).
--
-- Это отдельная миграция, а не правка 000001: golang-migrate помнит номера
-- уже применённых миграций, а не их содержимое. Если бы 000001 уже была
-- накатана (локально, в CI, у ревьювера) и мы бы просто изменили её файл,
-- migrate увидел бы номер 1 в своей таблице schema_migrations, посчитал бы
-- его уже выполненным и молча пропустил — база осталась бы со старыми
-- VARCHAR-колонками, а Go-код уже ждал бы ENUM. Новую схему можно
-- гарантированно раскатить только новым номером миграции.
CREATE TYPE upload_status_type AS ENUM ('uploading', 'completed', 'failed');
CREATE TYPE processing_status_type AS ENUM ('pending', 'processing', 'completed', 'failed');

ALTER TABLE avatars
    ALTER COLUMN upload_status DROP DEFAULT,
    ALTER COLUMN upload_status TYPE upload_status_type USING upload_status::upload_status_type,
    ALTER COLUMN upload_status SET DEFAULT 'uploading';

ALTER TABLE avatars
    ALTER COLUMN processing_status DROP DEFAULT,
    ALTER COLUMN processing_status TYPE processing_status_type USING processing_status::processing_status_type,
    ALTER COLUMN processing_status SET DEFAULT 'pending';
