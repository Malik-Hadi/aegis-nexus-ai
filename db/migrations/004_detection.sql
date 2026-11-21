-- Block 4: detection rules, alerts and incidents.
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS rule_id text;
ALTER TABLE alerts ALTER COLUMN event_id DROP NOT NULL;           -- DET-005 has no triggering event
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS agent_id uuid REFERENCES agents(id);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS incident_id uuid REFERENCES incidents(id);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS severity text;
CREATE UNIQUE INDEX IF NOT EXISTS alerts_event_rule_unique ON alerts (tenant_id, rule_id, event_id) WHERE event_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS alerts_incident ON alerts (incident_id);
CREATE INDEX IF NOT EXISTS events_payload_user ON events ((payload->>'user'));
CREATE INDEX IF NOT EXISTS events_payload_src ON events ((payload->>'source_ip'));

-- Documentation of the five rules (the logic itself lives in services/api/rules.go).
INSERT INTO rules (id, version, enabled, event_kind, criteria, severity) VALUES
 ('DET-001', 1, true, 'auth.failed_login',        '{"threshold":6,"window_minutes":5,"group_by":"payload.user"}', 'high'),
 ('DET-002', 1, true, 'privilege.group_added',    '{"privileged_groups":["administrators","domain admins","enterprise admins","schema admins","root","sudo","wheel"]}', 'high'),
 ('DET-003', 1, true, 'net.connection_failed',    '{"threshold":10,"min_distinct_ports":3,"window_minutes":5,"group_by":"payload.source_ip"}', 'medium'),
 ('DET-004', 1, true, 'process.executable_seen',  '{"match":"payload.sha256 in simulations/ioc_hashes.txt"}', 'high'),
 ('DET-005', 1, true, 'heartbeat.missing',        '{"stale_after_seconds":180}', 'medium')
ON CONFLICT DO NOTHING;
