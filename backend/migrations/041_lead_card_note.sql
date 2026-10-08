-- Комментарий на карточке кандидата (слот вместо «Компания» в канбане/карточке)
ALTER TABLE leads ADD COLUMN IF NOT EXISTS card_note TEXT NOT NULL DEFAULT '';
