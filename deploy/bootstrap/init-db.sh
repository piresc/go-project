#!/usr/bin/env bash
# Creates one login role and one schema per service inside $POSTGRES_DB.
#
# It runs once on a fresh volume (docker-entrypoint-initdb.d) and is idempotent,
# so `make db-init` can re-run it on an existing volume, for example after
# adding a service below. A re-run also updates the role passwords.
#
# Each role owns its schema and has its search_path set to it. PUBLIC loses
# access to the public schema and to connecting, and no role gets USAGE on
# another service's schema, so a cross-schema read fails with "permission
# denied".
set -euo pipefail

: "${POSTGRES_USER:?POSTGRES_USER is required}"
: "${POSTGRES_DB:?POSTGRES_DB is required}"
: "${USER_DB_PASSWORD:?USER_DB_PASSWORD is required}"
: "${ASSET_DB_PASSWORD:?ASSET_DB_PASSWORD is required}"

# The SQL goes in on stdin through a quoted heredoc and the passwords through
# psql variables, so nothing is interpolated into the SQL by the shell.
psql -v ON_ERROR_STOP=1 \
  -v db="$POSTGRES_DB" \
  -v user_pw="$USER_DB_PASSWORD" \
  -v asset_pw="$ASSET_DB_PASSWORD" \
  --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<'EOSQL'
REVOKE ALL ON SCHEMA public FROM PUBLIC;
REVOKE CONNECT ON DATABASE :"db" FROM PUBLIC;

-- user-service: role user_svc, schema users.
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', 'user_svc', :'user_pw')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'user_svc') \gexec
ALTER ROLE user_svc PASSWORD :'user_pw';
GRANT CONNECT ON DATABASE :"db" TO user_svc;
CREATE SCHEMA IF NOT EXISTS users AUTHORIZATION user_svc;
ALTER ROLE user_svc IN DATABASE :"db" SET search_path = users;

-- asset-service: role asset_svc, schema assets.
SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', 'asset_svc', :'asset_pw')
WHERE NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'asset_svc') \gexec
ALTER ROLE asset_svc PASSWORD :'asset_pw';
GRANT CONNECT ON DATABASE :"db" TO asset_svc;
CREATE SCHEMA IF NOT EXISTS assets AUTHORIZATION asset_svc;
ALTER ROLE asset_svc IN DATABASE :"db" SET search_path = assets;
EOSQL
