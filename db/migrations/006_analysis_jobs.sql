CREATE TABLE IF NOT EXISTS analysis_jobs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    requested_by text NOT NULL,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','completed','failed')),
    attempts integer NOT NULL DEFAULT 0,
    result jsonb,
    claimed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS analysis_jobs_active ON analysis_jobs (tenant_id, incident_id)
    WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS analysis_jobs_pending ON analysis_jobs (created_at) WHERE status IN ('queued','running');