-- Block 5: approvals (simulated containment) and audit lookups.
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS executed_at timestamptz;
ALTER TABLE approvals ADD COLUMN IF NOT EXISTS simulation_note text;
CREATE INDEX IF NOT EXISTS approvals_incident ON approvals (incident_id);
-- At most ONE pending approval per incident.
CREATE UNIQUE INDEX IF NOT EXISTS approvals_one_pending ON approvals (incident_id) WHERE decision = 'pending';
CREATE INDEX IF NOT EXISTS audit_target ON audit_logs (tenant_id, target);
