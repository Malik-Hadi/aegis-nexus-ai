-- Lab seed data: two tenants (needed for cross-tenant tests). No real secrets.
-- Users are created by the API/seed command so passwords are hashed properly.
INSERT INTO tenants (id, name) VALUES
  ('11111111-1111-1111-1111-111111111111', 'Lab Tenant A'),
  ('22222222-2222-2222-2222-222222222222', 'Lab Tenant B');
