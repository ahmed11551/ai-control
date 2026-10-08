-- Город у лида + флаг «МСК Башня» у РОПа (лиды только менеджерам своего отдела)
ALTER TABLE leads ADD COLUMN IF NOT EXISTS city TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS msk_tower BOOLEAN NOT NULL DEFAULT false;
