#!/bin/bash
set -e

# Create one database per service. Schemas are not created here: each service
# runs its own migrations on startup. Runs only on a fresh data volume.
for db in users assets; do
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres <<EOSQL
SELECT 'CREATE DATABASE $db'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$db')\gexec
EOSQL
done
