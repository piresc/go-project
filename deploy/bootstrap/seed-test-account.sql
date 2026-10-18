-- seed-test-account.sql
-- Inserts a well-known test account for local e2e. Run with `make seed` after
-- user-service has migrated the users database. Idempotent: re-running resets
-- the seeded refresh token so e2e can be repeated (its logout revokes it).
-- NEVER run this against production.

-- Test user
INSERT INTO users (id, email, display_name, role)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    'testuser@example.com',
    'Test User',
    'investor'
)
ON CONFLICT DO NOTHING;

-- Test auth provider (Google OAuth)
INSERT INTO auth_providers (user_id, provider, provider_subject)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    'google',
    'google-sub-12345'
)
ON CONFLICT DO NOTHING;

-- Test refresh token (SHA-256 of 'seed-refresh-token', expires in 30 days)
INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked)
VALUES (
    '22222222-2222-2222-2222-222222222222',
    '11111111-1111-1111-1111-111111111111',
    '94ddae8dc4a371cbdcc42ca2635623330272847ec68495b572d155d9bae902f0',
    NOW() + INTERVAL '30 days',
    FALSE
)
ON CONFLICT (id) DO UPDATE SET revoked = FALSE, expires_at = NOW() + INTERVAL '30 days';
