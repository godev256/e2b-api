#!/usr/bin/env bash
# Signs in as the Hydra demo user (foo@bar.com) by driving the real OAuth flow
# with curl: /auth/login -> Hydra -> demo login page -> consent -> /auth/callback.
# Prints only the access token on stdout, so it can be used as $(make login).
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
LOGIN_EMAIL="${LOGIN_EMAIL:-foo@bar.com}"
LOGIN_PASSWORD="${LOGIN_PASSWORD:-foobar}"
HYDRA_UI="${HYDRA_UI:-http://localhost:3000}"

jar="$(mktemp)"; page="$(mktemp)"
trap 'rm -f "$jar" "$page"' EXIT

fail() { echo "login failed: $*" >&2; exit 1; }
field() { grep -o "name=\"$1\" value=\"[^\"]*\"" "$page" | head -1 | sed 's/.*value="//; s/"$//' || true; }
token_in_page() { sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p' "$page"; }
curl_flow() { curl -sS --max-time 20 -c "$jar" -b "$jar" "$@"; }

# 1. Start at our API. It redirects to Hydra, which redirects to the login page.
curl_flow -L -o "$page" "$API_URL/auth/login" || fail "cannot reach $API_URL (is the stack up? try: make start)"
csrf="$(field _csrf)"; challenge="$(field challenge)"
[ -n "$csrf" ] && [ -n "$challenge" ] || fail "unexpected login page from $HYDRA_UI"

# 2. Submit credentials. Hydra then redirects to the consent page.
curl_flow -L -o "$page" "$HYDRA_UI/login" \
  --data-urlencode "_csrf=$csrf" --data-urlencode "challenge=$challenge" \
  --data-urlencode "email=$LOGIN_EMAIL" --data-urlencode "password=$LOGIN_PASSWORD" \
  --data-urlencode "remember=1" --data-urlencode "submit=Log in" \
  || fail "cannot reach the Hydra login page at $HYDRA_UI"

# 3. Grant consent. Hydra remembers earlier consent, so on a repeat login it
#    redirects straight to /auth/callback and this step is skipped.
token="$(token_in_page)"
if [ -z "$token" ]; then
  csrf="$(field _csrf)"; challenge="$(field challenge)"
  [ -n "$challenge" ] || fail "wrong email or password for $LOGIN_EMAIL (the demo login page did not offer consent)"
  curl_flow -L -o "$page" "$HYDRA_UI/consent" \
    --data-urlencode "_csrf=$csrf" --data-urlencode "challenge=$challenge" \
    --data-urlencode "grant_scope=openid" --data-urlencode "grant_scope=offline" \
    --data-urlencode "remember=1" --data-urlencode "submit=Allow access" \
    || fail "cannot reach the Hydra consent page at $HYDRA_UI"
  token="$(token_in_page)"
fi
[ -n "$token" ] || fail "no access token in callback response: $(head -c 300 "$page")"
printf '%s\n' "$token"
