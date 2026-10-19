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
echo "  go-project E2E Test Suite"
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

section "Portfolio"

STATUS="$(status_of "${BASE_URL}/api/v1/portfolio")"
assert_eq "GET /portfolio without token status" "401" "${STATUS}"

# The seed bypasses user.created, so the seeded user has no portfolio.
RAW="$(curl -s -w '\n%{http_code}' -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio")"
STATUS="$(echo "${RAW}" | tail -n1)"
RESP="$(echo "${RAW}" | sed '$d')"
assert_eq "GET /portfolio seeded user status" "404" "${STATUS}"
assert_eq "GET /portfolio seeded user message" "portfolio not found" "$(echo "${RESP}" | jq -r '.error')"

section "Assets"

STATUS="$(status_of -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/assets")"
assert_eq "GET /assets with token status" "200" "${STATUS}"

section "Prices"

QUOTES_FILE="$(dirname "${BASH_SOURCE[0]}")/market-mock/quotes.json"
EXPECTED_TOP="$(jq -r '.quotes | max_by(.change_pct) | .symbol' "${QUOTES_FILE}")"

STATUS="$(status_of "${BASE_URL}/api/v1/prices/top-performers")"
assert_eq "GET /prices/top-performers without token status" "200" "${STATUS}"

# The first refresh runs on the schedule, so poll until the cache is filled.
RESP=""
for _ in $(seq 1 30); do
  RESP="$(curl -s "${BASE_URL}/api/v1/prices/top-performers")"
  if [ "$(echo "${RESP}" | jq -r '.data | length' 2>/dev/null || true)" = "5" ]; then
    break
  fi
  sleep 1
done
assert_eq "GET /prices/top-performers returns 5 items" "5" "$(echo "${RESP}" | jq -r '.data | length')"
assert_eq "GET /prices/top-performers first item is highest change_pct" "${EXPECTED_TOP}" "$(echo "${RESP}" | jq -r '.data[0].symbol')"

RAW="$(curl -s -w '\n%{http_code}' "${BASE_URL}/api/v1/prices/quotes?symbols=NVDA,AAPL")"
STATUS="$(echo "${RAW}" | tail -n1)"
RESP="$(echo "${RAW}" | sed '$d')"
assert_eq "GET /prices/quotes status" "200" "${STATUS}"
assert_eq "GET /prices/quotes returns 2 quotes" "2" "$(echo "${RESP}" | jq -r '.data | length')"
assert_eq "GET /prices/quotes keeps requested order" "NVDA,AAPL" "$(echo "${RESP}" | jq -r '[.data[].symbol] | join(",")')"
NVDA_PRICE="$(echo "${RESP}" | jq -r '.data[0].price')"

STATUS="$(status_of "${BASE_URL}/api/v1/prices/quotes")"
assert_eq "GET /prices/quotes without symbols status" "400" "${STATUS}"

section "Holdings"

PUT_JSON() {
  curl -s -w '\n%{http_code}' -X PUT "${BASE_URL}/api/v1/portfolio/holdings/$1" -H "${AUTH_HEADER}" -H "Content-Type: application/json" -d "$2"
}

RAW="$(PUT_JSON NVDA '{"quantity":2}')"
assert_eq "PUT /portfolio/holdings/NVDA status" "200" "$(echo "${RAW}" | tail -n1)"

RAW="$(PUT_JSON NVDA '{"quantity":0}')"
assert_eq "PUT /portfolio/holdings/NVDA quantity 0 status" "400" "$(echo "${RAW}" | tail -n1)"

RESP="$(curl -s -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio")"
assert_eq "GET /portfolio includes NVDA holding" "NVDA" "$(echo "${RESP}" | jq -r '.data.holdings[] | select(.symbol == "NVDA") | .symbol')"

RAW="$(curl -s -w '\n%{http_code}' -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio/value")"
STATUS="$(echo "${RAW}" | tail -n1)"
RESP="$(echo "${RAW}" | sed '$d')"
assert_eq "GET /portfolio/value status" "200" "${STATUS}"
assert_eq "GET /portfolio/value total is 2 x NVDA price" "$(awk -v p="${NVDA_PRICE}" 'BEGIN { printf "%.2f", 2 * p }')" "$(echo "${RESP}" | jq -r '.data.total' | awk '{ printf "%.2f", $1 }')"

RAW="$(PUT_JSON ZZZZ '{"quantity":1}')"
assert_eq "PUT /portfolio/holdings/ZZZZ status" "200" "$(echo "${RAW}" | tail -n1)"
RESP="$(curl -s -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio/value")"
assert_eq "GET /portfolio/value lists ZZZZ as unpriced" "ZZZZ" "$(echo "${RESP}" | jq -r '.data.unpriced[]')"

STATUS="$(status_of -X DELETE -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio/holdings/ZZZZ")"
assert_eq "DELETE /portfolio/holdings/ZZZZ status" "204" "${STATUS}"
STATUS="$(status_of -X DELETE -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio/holdings/ZZZZ")"
assert_eq "DELETE /portfolio/holdings/ZZZZ again status" "404" "${STATUS}"

# These checks need the local compose stack, so they are skipped for remote runs.
if [ "${E2E_DOCKER:-0}" = "1" ]; then
  COMPOSE_FILE_PATH="$(dirname "${BASH_SOURCE[0]}")/docker-compose.yml"
  compose() { docker compose -f "${COMPOSE_FILE_PATH}" "$@"; }

  section "Datastore isolation"

  assert_eq "asset-service Redis has no price cache" "0" "$(compose exec -T redis-asset redis-cli EXISTS prices:quotes | tr -d '\r')"
  assert_eq "price-service Redis has the price cache" "1" "$(compose exec -T redis-price redis-cli EXISTS prices:quotes | tr -d '\r')"

  section "Circuit breaker"

  trap 'compose up -d --wait price-service >/dev/null 2>&1' EXIT
  compose stop price-service >/dev/null 2>&1
  STATUS="$(status_of -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio/value")"
  compose up -d --wait price-service >/dev/null 2>&1
  assert_eq "GET /portfolio/value with price-service down status" "503" "${STATUS}"
fi

section "Logout"

RESP="$(curl -s -X POST "${BASE_URL}/api/v1/auth/logout" -H "${AUTH_HEADER}")"
assert_eq "POST /auth/logout status field" "success" "$(echo "${RESP}" | jq -r '.status')"
assert_eq "POST /auth/logout message" "logged out successfully" "$(echo "${RESP}" | jq -r '.data.message')"

STATUS="$(status_of -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/users/me")"
assert_eq "GET /users/me after logout status" "401" "${STATUS}"
RESP="$(curl -s -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/users/me")"
assert_eq "GET /users/me after logout message" "token revoked" "$(echo "${RESP}" | jq -r '.error')"

# asset-service checks revocation against user-service's Redis (split stores).
STATUS="$(status_of -H "${AUTH_HEADER}" "${BASE_URL}/api/v1/portfolio")"
assert_eq "GET /portfolio after logout status" "401" "${STATUS}"

printf '\nE2E complete (%d/%d checks passed)\n' "${PASS}" "${TOTAL}"
