-- AEGIS NEXUS AI core schema (PDF section 04). Every domain table carries tenant_id.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  subject text NOT NULL,
  password_hash text,
  role text NOT NULL CHECK (role IN ('admin','analyst')),
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
  UNIQUE (tenant_id, subject)
);

CREATE TABLE agents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  hostname text NOT NULL,
  os text NOT NULL CHECK (os IN ('windows','linux')),
  last_seen_at timestamptz,
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
  credential_hash text NOT NULL
);

CREATE TABLE events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  agent_id uuid NOT NULL REFERENCES agents(id),
  occurred_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  kind text NOT NULL,
  severity text NOT NULL DEFAULT 'info',
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  source_ref text,
  idempotency_key text NOT NULL,
  UNIQUE (tenant_id, agent_id, idempotency_key)
);
CREATE INDEX events_tenant_time ON events (tenant_id, occurred_at DESC);
CREATE INDEX events_kind ON events (tenant_id, kind);

CREATE TABLE rules (
  id text NOT NULL,
  version int NOT NULL DEFAULT 1,
  enabled boolean NOT NULL DEFAULT true,
  event_kind text NOT NULL,
  criteria jsonb NOT NULL,
  severity text NOT NULL,
  PRIMARY KEY (id, version)
);

CREATE TABLE incidents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  title text NOT NULL,
  severity text NOT NULL,
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','investigating','resolved','closed')),
  owner_id uuid REFERENCES users(id),
  dedup_key text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
-- One open incident per dedup key (supports DET-001 deduplication).
CREATE UNIQUE INDEX incidents_dedup_open ON incidents (tenant_id, dedup_key)
  WHERE dedup_key IS NOT NULL AND status IN ('open','investigating');

CREATE TABLE alerts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  event_id uuid NOT NULL REFERENCES events(id),
  rule_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL DEFAULT 'new'
);

CREATE TABLE incident_events (
  incident_id uuid NOT NULL REFERENCES incidents(id),
  event_id uuid NOT NULL REFERENCES events(id),
  PRIMARY KEY (incident_id, event_id)
);

CREATE TABLE approvals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  incident_id uuid NOT NULL REFERENCES incidents(id),
  action text NOT NULL,
  requester uuid NOT NULL REFERENCES users(id),
  approver uuid REFERENCES users(id),
  decision text NOT NULL DEFAULT 'pending' CHECK (decision IN ('pending','approved','denied')),
  decided_at timestamptz
);

CREATE TABLE audit_logs (
  id bigserial PRIMARY KEY,
  tenant_id uuid REFERENCES tenants(id),
  actor text NOT NULL,
  action text NOT NULL,
  target text,
  timestamp timestamptz NOT NULL DEFAULT now(),
  result text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb
);

-- Append-only: block UPDATE and DELETE on audit_logs.
CREATE FUNCTION audit_logs_immutable() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION 'audit_logs is append-only';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER audit_logs_no_change BEFORE UPDATE OR DELETE ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();
