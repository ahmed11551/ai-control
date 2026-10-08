-- Один лид = одна сделка (убираем дубли от гонки ensureDealForLead)
DELETE FROM deals a
USING deals b
WHERE a.lead_id IS NOT NULL
  AND a.lead_id = b.lead_id
  AND a.created_at > b.created_at;

UPDATE leads l
SET converted_deal_id = d.id
FROM deals d
WHERE d.lead_id = l.id
  AND (l.converted_deal_id IS NULL OR l.converted_deal_id <> d.id);

CREATE UNIQUE INDEX IF NOT EXISTS deals_one_per_lead_uidx
  ON deals (lead_id)
  WHERE lead_id IS NOT NULL;
