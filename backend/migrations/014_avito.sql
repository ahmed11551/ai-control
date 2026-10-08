CREATE UNIQUE INDEX IF NOT EXISTS leads_source_ext_uidx
    ON leads (source, vk_lead_id)
    WHERE vk_lead_id IS NOT NULL AND vk_lead_id <> '';
