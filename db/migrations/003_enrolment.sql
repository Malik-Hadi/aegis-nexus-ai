-- One-time enrolment tokens (Block 2). Only the hash is stored.
CREATE TABLE IF NOT EXISTS enrolment_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES tenants(id),
  token_hash text NOT NULL UNIQUE,
  created_by uuid REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at timestamptz
);
