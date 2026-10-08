-- Метаданные стадий воронок (короткое имя для канбана + полное описание).
ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS short_name TEXT NOT NULL DEFAULT '';
ALTER TABLE pipeline_stages ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
