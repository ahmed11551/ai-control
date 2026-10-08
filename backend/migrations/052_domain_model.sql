-- ТЗ v6.0: единая доменная модель кандидата и сквозные пакеты передачи.
-- На первом этапе связи с legacy leads/deals nullable: существующие сценарии
-- продолжают работать, пока данные и API переводятся на candidate_id.

CREATE TABLE IF NOT EXISTS candidates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legacy_contact_id UUID REFERENCES contacts(id) ON DELETE SET NULL,
    display_name TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'crm',
    status TEXT NOT NULL DEFAULT 'active',
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS candidates_legacy_contact_uidx
    ON candidates (legacy_contact_id) WHERE legacy_contact_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS candidates_status_idx
    ON candidates (status, updated_at DESC);

ALTER TABLE leads ADD COLUMN IF NOT EXISTS candidate_id UUID REFERENCES candidates(id) ON DELETE SET NULL;
ALTER TABLE deals ADD COLUMN IF NOT EXISTS candidate_id UUID REFERENCES candidates(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS leads_candidate_idx ON leads (candidate_id);
CREATE INDEX IF NOT EXISTS deals_candidate_idx ON deals (candidate_id);

CREATE OR REPLACE FUNCTION ensure_candidate_link()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    candidate_uuid UUID;
    linked_candidate UUID;
    bound_contact UUID;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF OLD.candidate_id IS NOT NULL AND NEW.candidate_id IS DISTINCT FROM OLD.candidate_id THEN
            RAISE EXCEPTION 'candidate identity is immutable' USING ERRCODE = '23514';
        END IF;
    END IF;
    candidate_uuid := NEW.candidate_id;
    IF TG_TABLE_NAME = 'leads' THEN
        IF NEW.converted_deal_id IS NOT NULL THEN
            SELECT candidate_id INTO linked_candidate FROM deals WHERE id = NEW.converted_deal_id;
            IF linked_candidate IS NOT NULL THEN
                IF candidate_uuid IS NOT NULL AND candidate_uuid <> linked_candidate THEN
                    RAISE EXCEPTION 'lead and converted deal identities differ' USING ERRCODE = '23514';
                END IF;
                candidate_uuid := linked_candidate;
            END IF;
        END IF;
    END IF;
    IF TG_TABLE_NAME = 'deals' THEN
        IF NEW.lead_id IS NOT NULL THEN
            SELECT candidate_id INTO linked_candidate FROM leads WHERE id = NEW.lead_id FOR UPDATE;
            IF NOT FOUND THEN
                RAISE EXCEPTION 'linked lead does not exist' USING ERRCODE = '23503';
            END IF;
            IF linked_candidate IS NULL THEN
                UPDATE leads SET candidate_id = candidate_uuid WHERE id = NEW.lead_id
                RETURNING candidate_id INTO linked_candidate;
            END IF;
            IF candidate_uuid IS NOT NULL AND candidate_uuid <> linked_candidate THEN
                RAISE EXCEPTION 'deal and lead candidate identities differ' USING ERRCODE = '23514';
            END IF;
            candidate_uuid := linked_candidate;
        END IF;
    END IF;
    IF candidate_uuid IS NULL THEN
        INSERT INTO candidates (legacy_contact_id, display_name, source)
        VALUES (NEW.contact_id, coalesce(NEW.title, ''), TG_TABLE_NAME)
        ON CONFLICT (legacy_contact_id) WHERE legacy_contact_id IS NOT NULL
        DO UPDATE SET updated_at = now()
        RETURNING id INTO candidate_uuid;
    END IF;
    SELECT legacy_contact_id INTO bound_contact FROM candidates WHERE id = candidate_uuid FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'candidate does not exist' USING ERRCODE = '23503';
    END IF;
    IF NEW.contact_id IS NOT NULL THEN
        IF bound_contact IS NOT NULL AND bound_contact <> NEW.contact_id THEN
            RAISE EXCEPTION 'candidate belongs to another contact' USING ERRCODE = '23514';
        END IF;
        -- The unique index also serializes competing claims on the same contact.
        IF bound_contact IS NULL THEN
            UPDATE candidates SET legacy_contact_id = NEW.contact_id WHERE id = candidate_uuid;
        END IF;
    END IF;
    NEW.candidate_id := candidate_uuid;
    RETURN NEW;
END;
$$;
CREATE OR REPLACE FUNCTION protect_candidate_identity()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR
       (OLD.legacy_contact_id IS NOT NULL AND NEW.legacy_contact_id IS DISTINCT FROM OLD.legacy_contact_id) THEN
        RAISE EXCEPTION 'candidate identity is immutable' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS candidates_identity ON candidates;
CREATE TRIGGER candidates_identity BEFORE UPDATE OF id, legacy_contact_id ON candidates
FOR EACH ROW EXECUTE FUNCTION protect_candidate_identity();
DROP TRIGGER IF EXISTS leads_candidate_link ON leads;
CREATE TRIGGER leads_candidate_link
BEFORE INSERT OR UPDATE OF contact_id, title, candidate_id, converted_deal_id ON leads
FOR EACH ROW EXECUTE FUNCTION ensure_candidate_link();
DROP TRIGGER IF EXISTS deals_candidate_link ON deals;
CREATE TRIGGER deals_candidate_link
BEFORE INSERT OR UPDATE OF contact_id, title, candidate_id, lead_id ON deals
FOR EACH ROW EXECUTE FUNCTION ensure_candidate_link();

-- Preserve already assigned identities. Conflicts require explicit reconciliation;
-- never silently merge candidates that may own documents or approval history.
-- First propagate existing deal identities to linked, unassigned leads.
UPDATE leads l SET candidate_id = d.candidate_id FROM deals d
WHERE (d.lead_id = l.id OR l.converted_deal_id = d.id)
  AND l.candidate_id IS NULL AND d.candidate_id IS NOT NULL;
UPDATE leads SET candidate_id = candidate_id WHERE candidate_id IS NULL;
UPDATE deals SET candidate_id = candidate_id;
-- Validate existing contact bindings as well as new ones.
UPDATE leads SET candidate_id = candidate_id;
ALTER TABLE leads ALTER COLUMN candidate_id SET NOT NULL;
ALTER TABLE deals ALTER COLUMN candidate_id SET NOT NULL;

-- Identity links must reject deletion, not attempt to NULL an immutable,
-- mandatory identity. Replace legacy FK actions on upgrades as well.
ALTER TABLE candidates DROP CONSTRAINT IF EXISTS candidates_legacy_contact_id_fkey;
ALTER TABLE candidates ADD CONSTRAINT candidates_legacy_contact_id_fkey
    FOREIGN KEY (legacy_contact_id) REFERENCES contacts(id) ON DELETE RESTRICT;
ALTER TABLE leads DROP CONSTRAINT IF EXISTS leads_candidate_id_fkey;
ALTER TABLE leads ADD CONSTRAINT leads_candidate_id_fkey
    FOREIGN KEY (candidate_id) REFERENCES candidates(id) ON DELETE RESTRICT;
ALTER TABLE deals DROP CONSTRAINT IF EXISTS deals_candidate_id_fkey;
ALTER TABLE deals ADD CONSTRAINT deals_candidate_id_fkey
    FOREIGN KEY (candidate_id) REFERENCES candidates(id) ON DELETE RESTRICT;

CREATE TABLE IF NOT EXISTS candidate_trips (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'planned',
    transport_type TEXT NOT NULL DEFAULT '',
    origin_city TEXT NOT NULL DEFAULT '',
    destination_city TEXT NOT NULL DEFAULT '',
    depart_at TIMESTAMPTZ,
    arrive_at TIMESTAMPTZ,
    ticket_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS candidate_trips_candidate_idx
    ON candidate_trips (candidate_id, arrive_at DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS candidate_trips_arrival_idx
    ON candidate_trips (destination_city, arrive_at);

CREATE TABLE IF NOT EXISTS candidate_documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    document_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'uploaded',
    original_filename TEXT NOT NULL DEFAULT '',
    encrypted_storage_key TEXT NOT NULL,
    encrypted_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    uploaded_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS candidate_documents_candidate_idx
    ON candidate_documents (candidate_id, document_type, created_at DESC);

CREATE TABLE IF NOT EXISTS approval_requests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE CASCADE,
    deal_id UUID REFERENCES deals(id) ON DELETE SET NULL,
    version INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    prepared_by UUID REFERENCES users(id) ON DELETE SET NULL,
    manager_comment TEXT NOT NULL DEFAULT '',
    return_reason TEXT NOT NULL DEFAULT '',
    submitted_at TIMESTAMPTZ,
    reviewed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (candidate_id, version)
);
CREATE INDEX IF NOT EXISTS approval_requests_queue_idx
    ON approval_requests (status, created_at DESC);
CREATE INDEX IF NOT EXISTS approval_requests_candidate_idx
    ON approval_requests (candidate_id, version DESC);

CREATE TABLE IF NOT EXISTS approval_request_documents (
    approval_request_id UUID NOT NULL REFERENCES approval_requests(id) ON DELETE CASCADE,
    document_id UUID NOT NULL REFERENCES candidate_documents(id) ON DELETE RESTRICT,
    PRIMARY KEY (approval_request_id, document_id)
);

CREATE TABLE IF NOT EXISTS customer_packages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_request_id UUID NOT NULL REFERENCES approval_requests(id) ON DELETE RESTRICT,
    candidate_id UUID NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
    customer_site_id UUID REFERENCES customer_sites(id) ON DELETE RESTRICT,
    object_id TEXT NOT NULL DEFAULT '',
    version INT NOT NULL,
    status TEXT NOT NULL DEFAULT 'published',
    approved_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    published_by UUID REFERENCES users(id) ON DELETE SET NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (approval_request_id, version)
);
CREATE INDEX IF NOT EXISTS customer_packages_scope_idx
    ON customer_packages (customer_site_id, object_id, status, published_at DESC);
