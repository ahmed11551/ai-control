-- Расписание частоты скорозвона по возрасту лида (дни).
CREATE TABLE IF NOT EXISTS autodial_schedule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sort_order INT NOT NULL DEFAULT 0,
    day_from INT NOT NULL,
    day_to INT,
    calls_min INT NOT NULL DEFAULT 0,
    calls_max INT NOT NULL DEFAULT 0,
    interval_minutes INT NOT NULL DEFAULT 60,
    note TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS autodial_schedule_day_from_uidx
  ON autodial_schedule (day_from);

INSERT INTO autodial_schedule (sort_order, day_from, day_to, calls_min, calls_max, interval_minutes, note)
SELECT * FROM (VALUES
  (1, 1, 1, 5, 15, 60, 'Максимальная активность в первый день'),
  (2, 2, 2, 1, 7, 120, 'Снижаем частоту, но сохраняем активность'),
  (3, 3, 3, 1, 5, 180, 'Продолжаем работу с теплыми лидами'),
  (4, 4, 7, 3, 4, 240, 'Умеренная активность'),
  (5, 8, 30, 1, 1, 1440, 'Разное время суток'),
  (6, 31, NULL, 0, 0, 1440, 'Можно настроить индивидуально')
) AS v(sort_order, day_from, day_to, calls_min, calls_max, interval_minutes, note)
WHERE NOT EXISTS (SELECT 1 FROM autodial_schedule LIMIT 1);
