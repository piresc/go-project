#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
TEST_EMAIL="${TEST_EMAIL:-testuser@example.com}"
SEEDED_REFRESH_TOKEN="${SEEDED_REFRESH_TOKEN:-seed-refresh-token}"

PASS=0
TOTAL=0

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

section() {
  printf '\n%b--- %s ---%b\n' "${YELLOW}" "$1" "${NC}"
}

pass() {
  PASS=$((PASS + 1))
  TOTAL=$((TOTAL + 1))
  printf '%bPASS%b %s\n' "${GREEN}" "${NC}" "$1"
}

fail() {
  TOTAL=$((TOTAL + 1))
  printf '%bFAIL%b %s\nExpected: %s\nActual: %s\n' "${RED}" "${NC}" "$1" "$2" "$3" >&2
  exit 1
}

assert_eq() {
  local name="$1"
  local expected="$2"
  local actual="$3"
  if [ "$expected" != "$actual" ]; then
    fail "$name" "$expected" "$actual"
  fi
  pass "$name"
}

status_of() {
  curl -s -o /dev/null -w "%{http_code}" "$@"
}

require_cmd curl
require_cmd jq

echo "============================================"
echo "  go-project E2E Test Suite (user-service)"
echo "============================================"

section "Auth"

STATUS="$(status_of -X POST "${BASE_URL}/api/v1/auth/google" -H "Content-Type: application/json" -d '{"id_token":"invalid-token"}')"
assert_eq "POST /auth/google invalid token status" "401" "${STATUS}"
RESP="$(curl -s -X POST "${BASE_URL}/api/v1/auth/google" -H "Content-Type: application/json" -d '{"id_token":"invalid-token"}')"
assert_eq "POST /auth/google invalid token status field" "error" "$(echo "${RESP}" | jq -r '.status')"
assert_eq "POST /auth/google invalid token message" "invalid token" "$(echo "${RESP}" | jq -r '.error')"

STATUS="$(status_of -X POST "${BASE_URL}/api/v1/auth/apple" -H "Content-Type: application/json" -d '{"id_token":"invalid-token"}')"
assert_eq "POST /auth/apple invalid token status" "401" "${STATUS}"
RESP="$(curl -s -X POST "${BASE_URL}/api/v1/auth/apple" -H "Content-Type: application/json" -d '{"id_token":"invalid-token"}')"
assert_eq "POST /auth/apple invalid token message" "invalid token" "$(echo "${RESP}" | jq -r '.error')"

# The refresh rotates the seeded token, so run `make seed` before each e2e run.
RAW="$(curl -s -w '\n%{http_code}' -X POST "${BASE_URL}/api/v1/auth/refresh" -H "Content-Type: application/json" -d "{\"refresh_token\":\"${SEEDED_REFRESH_TOKEN}\"}")"
STATUS="$(echo "${RAW}" | tail -n1)"
RESP="$(echo "${RAW}" | sed '$d')"
assert_eq "POST /auth/refresh seeded token status" "200" "${STATUS}"
assert_eq "POST /auth/refresh status field" "success" "$(echo "${RESP}" | jq -r '.status')"
ACCESS_TOKEN="$(echo "${RESP}" | jq -r '.data.access_token')"
ROTATED_REFRESH_TOKEN="$(echo "${RESP}" | jq -r '.data.refresh_token')"
EXPIRES_IN="$(echo "${RESP}" | jq -r '.data.expires_in')"
if [ -z "${ACCESS_TOKEN}" ] || [ "${ACCESS_TOKEN}" = "null" ]; then
  fail "POST /auth/refresh returns access token" "non-empty token" "${ACCESS_TOKEN}"
fi
pass "POST /auth/refresh returns access token"
if [ -z "${ROTATED_REFRESH_TOKEN}" ] || [ "${ROTATED_REFRESH_TOKEN}" = "null" ]; then
  fail "POST /auth/refresh returns rotated refresh token" "non-empty token" "${ROTATED_REFRESH_TOKEN}"
fi
pass "POST /auth/refresh returns rotated refresh token"
if [ -z "${EXPIRES_IN}" ] || [ "${EXPIRES_IN}" = "null" ]; then
  fail "POST /auth/refresh returns expires_in" "non-empty value" "${EXPIRES_IN}"
fi
pass "POST /auth/refresh returns expires_in"

AUTH_HEADER="Authorization: Bearer ${ACCESS_TOKEN}"

section "Profile"

STATUS="$(status_of "${BASE_URL}/api/v1/users/me")"
assert_eq "GET /users/me without token status" "401" "${STATUS}"
RESP="$(curl -s "${BASE_URL}/api/v1/users/me")"
assert_eq "GET /users/me without token message" "authentication required" "$(echo "${RESP}" | jq -r '.error')"

RESP="$(curl -s -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/users/me")"
assert_eq "GET /users/me status field" "success" "$(echo "${RESP}" | jq -r '.status')"
assert_eq "GET /users/me returns seeded email" "${TEST_EMAIL}" "$(echo "${RESP}" | jq -r '.data.email')"

RESP="$(curl -s -X PATCH "${BASE_URL}/api/v1/users/me" -H "${AUTH_HEADER}" -H "Content-Type: application/json" -d '{"display_name":"Updated Name"}')"
assert_eq "PATCH /users/me status field" "success" "$(echo "${RESP}" | jq -r '.status')"
assert_eq "PATCH /users/me persists display name" "Updated Name" "$(echo "${RESP}" | jq -r '.data.display_name')"

section "Logout"

RESP="$(curl -s -X POST "${BASE_URL}/api/v1/auth/logout" -H "${AUTH_HEADER}")"
assert_eq "POST /auth/logout status field" "success" "$(echo "${RESP}" | jq -r '.status')"
assert_eq "POST /auth/logout message" "logged out successfully" "$(echo "${RESP}" | jq -r '.data.message')"

STATUS="$(status_of -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/users/me")"
assert_eq "GET /users/me after logout status" "401" "${STATUS}"
RESP="$(curl -s -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/users/me")"
assert_eq "GET /users/me after logout message" "token revoked" "$(echo "${RESP}" | jq -r '.error')"

printf '\nE2E complete (%d/%d checks passed)\n' "${PASS}" "${TOTAL}"
