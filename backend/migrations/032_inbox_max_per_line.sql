-- MAX: один тред на аккаунт (line) × телефон клиента.
-- Старые треды с line_id и external_id=phone → line_id:phone.
-- При коллизиях сообщения сливаются в один тред.

DO $$
DECLARE
  r RECORD;
  phone_norm TEXT;
  tgt TEXT;
  keeper UUID;
BEGIN
  FOR r IN
    SELECT t.id, t.line_id, coalesce(t.phone,'') AS phone, t.external_id, t.last_at
    FROM inbox_threads t
    WHERE t.channel = 'max'
      AND t.line_id IS NOT NULL
      AND position(':' in t.external_id) = 0
    ORDER BY t.last_at DESC NULLS LAST, t.id
  LOOP
    phone_norm := regexp_replace(coalesce(nullif(r.phone,''), r.external_id), '\D', '', 'g');
    IF length(phone_norm) = 11 AND left(phone_norm, 1) = '8' THEN
      phone_norm := '7' || substr(phone_norm, 2);
    END IF;
    IF length(phone_norm) < 10 THEN
      CONTINUE;
    END IF;
    tgt := r.line_id::text || ':' || phone_norm;

    SELECT id INTO keeper
    FROM inbox_threads
    WHERE channel = 'max' AND external_id = tgt AND id <> r.id
    LIMIT 1;

    IF keeper IS NOT NULL THEN
      UPDATE inbox_messages SET thread_id = keeper WHERE thread_id = r.id;
      DELETE FROM inbox_threads WHERE id = r.id;
    ELSE
      BEGIN
        UPDATE inbox_threads
        SET external_id = tgt, phone = phone_norm
        WHERE id = r.id;
      EXCEPTION WHEN unique_violation THEN
        SELECT id INTO keeper
        FROM inbox_threads
        WHERE channel = 'max' AND external_id = tgt AND id <> r.id
        LIMIT 1;
        IF keeper IS NOT NULL THEN
          UPDATE inbox_messages SET thread_id = keeper WHERE thread_id = r.id;
          DELETE FROM inbox_threads WHERE id = r.id;
        END IF;
      END;
    END IF;
  END LOOP;
END $$;

UPDATE inbox_threads
SET phone = split_part(external_id, ':', 2)
WHERE channel = 'max'
  AND position(':' in external_id) > 0
  AND (phone IS NULL OR phone = '' OR phone = external_id OR position(':' in coalesce(phone,'')) > 0
       OR phone <> split_part(external_id, ':', 2));
