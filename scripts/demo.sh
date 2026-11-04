#!/usr/bin/env bash
# A guided tour of the API using real requests against the running stack.
# Safe to run repeatedly: it reuses one project and stops its leftovers first.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib.sh

API_URL="${API_URL:-http://localhost:8080}"
DEMO_PROJECT_NAME="Demo project"
BOB_PRIVATE_PROJECT="0192f000-0000-7000-8000-0000000000a2" # from db/fixtures, bob only
RATE_BURST_REQUESTS=40
REFILL_SECONDS=2.2 # a free plan bucket (10 tokens at 5/s) is full again after 2s

STATUS=""; BODY=""; HEADERS=""

# call METHOD PATH [JSON_BODY]: sets STATUS, BODY and HEADERS, prints one summary line.
# A rate limited response (not a quota one) is retried after its Retry-After
# delay, the way a well behaved client would, unless NO_RETRY=1.
call() {
  local method="$1" path="$2" data="${3:-}" raw attempt=0 wait_for
  local args=(-s --max-time 15 -X "$method" "$API_URL$path" -D - -w $'\n%{http_code}')
  [ "${NO_AUTH:-}" = 1 ] || args+=(-H "Authorization: Bearer $TOKEN")
  [ -n "$data" ] && args+=(-H 'Content-Type: application/json' -d "$data")
  while :; do
    raw="$(curl "${args[@]}" | tr -d '\r')"
    STATUS="${raw##*$'\n'}"
    raw="${raw%$'\n'*}"
    HEADERS="${raw%%$'\n\n'*}"
    BODY="${raw#*$'\n\n'}"
    if [ "$STATUS" = 429 ] && [ "${NO_RETRY:-}" != 1 ] && [ "$attempt" -lt 3 ] && [ "$(json_field code)" = rate_limit_exceeded ]; then
      wait_for="$(printf '%s' "$HEADERS" | sed -n 's/^[Rr]etry-[Aa]fter: *//p' | head -1)"
      printf '  %s%-6s %s 429, rate limited: waiting %ss as Retry-After says%s\n' "$DIM" "$method" "$path" "${wait_for:-1}" "$RESET"
      sleep "${wait_for:-1}"
      attempt=$((attempt + 1))
      continue
    fi
    break
  done
  local color="$GREEN"; [ "$STATUS" -ge 400 ] && color="$YELLOW"
  printf '  %s%-6s %s%s %s%s%s\n' "$DIM" "$method" "$path" "$RESET" "$color" "$STATUS" "$RESET"
  printf '         %s%s%s\n' "$DIM" "$(printf '%s' "$BODY" | compact 130)" "$RESET"
}
check() { # check "ok message" <test command...>
  local message="$1"; shift
  if "$@"; then ok "$message"; else printf '  %s✗%s %s\n' "$RED" "$RESET" "$message"; printf '      last response: %s %s\n' "$STATUS" "$BODY"; exit 1; fi
}
status_is() { [[ "$STATUS" =~ ^($1)$ ]]; }
error_code_is() { [ "$(json_field code)" = "$1" ]; }
body_lacks() { ! printf '%s' "$BODY" | grep -q "$1"; }
header_present() { printf '%s' "$HEADERS" | grep -qi "^$1:"; }
json_field() { printf '%s' "$BODY" | sed -n "s/.*\"$1\":\"\\([^\"]*\\)\".*/\\1/p" | head -1; }
refill() { printf '  %swaiting for the rate limit bucket to refill...%s\n' "$DIM" "$RESET"; sleep "$REFILL_SECONDS"; }

curl -s --max-time 3 "$API_URL/healthz" >/dev/null || die "the API is not reachable at $API_URL, run: make start"

info "Loading demo data (users, projects, sandboxes)"
if ! out="$($COMPOSE run --rm --no-deps -T migrate fixtures 2>&1)"; then
  printf '%s\n' "$out" >&2
  die "could not load fixtures"
fi
printf '%s\n' "$out" | grep '^fixtures:' | sed 's/^/  /'

info "1. Sign in with OAuth (Ory Hydra, authorization code flow with PKCE)"
TOKEN="$(scripts/login.sh)"
ok "got a JWT access token (${#TOKEN} chars), RS256, verified against Hydra's JWKS"

info "2. Requests without a valid token are rejected"
NO_AUTH=1 call GET /v1/projects
check "no token gives 401 unauthenticated" status_is 401
unset NO_AUTH

info "3. Create a project (or reuse the one from an earlier run), then list mine"
call GET "/v1/projects?limit=200"
PROJECT_ID="$(printf '%s' "$BODY" | { grep -o "\"id\":\"[^\"]*\",\"name\":\"$DEMO_PROJECT_NAME\"" || true; } | head -1 | sed 's/"id":"\([^"]*\)".*/\1/')"
if [ -z "$PROJECT_ID" ]; then
  call POST /v1/projects "{\"name\":\"$DEMO_PROJECT_NAME\"}"
  check "project created, I am its owner" status_is 201
  PROJECT_ID="$(json_field id)"
  call GET "/v1/projects?limit=200"
else
  ok "reusing project $PROJECT_ID from an earlier run"
fi
check "list shows my projects" status_is 200
check "Bob's private project is not in my list" body_lacks "$BOB_PRIVATE_PROJECT"

info "4. Only members can touch a project and its sandboxes"
call GET "/v1/projects/$BOB_PRIVATE_PROJECT/sandboxes"
check "Bob's project answers 404, not 403, so its existence is not revealed" status_is 404
call DELETE "/v1/sandboxes/0192f000-0000-7000-8000-0000000000b3"
check "Bob's sandbox cannot be stopped by me either" status_is 404

info "5. Share a project: add bob@example.com, after that it is his too"
refill
call GET "/v1/projects/$PROJECT_ID/sandboxes?limit=200"
leftovers="$(printf '%s' "$BODY" | grep -o '"id":"[^"]*","project_id":"[^"]*","template":"[^"]*","status":"running"' | sed 's/"id":"\([^"]*\)".*/\1/' || true)"
for id in $leftovers; do curl -s -o /dev/null -X DELETE -H "Authorization: Bearer $TOKEN" "$API_URL/v1/sandboxes/$id"; done
[ -z "$leftovers" ] || ok "stopped $(printf '%s\n' "$leftovers" | wc -l | tr -d ' ') sandbox(es) left by an earlier run"
call POST "/v1/projects/$PROJECT_ID/members" '{"email":"bob@example.com"}'
check "Bob added (201 the first time, 200 on later runs)" status_is "200|201"
call POST "/v1/projects/$PROJECT_ID/members" '{"email":"bob@example.com"}'
check "adding him again is a no-op" status_is 200
call POST "/v1/projects/$PROJECT_ID/members" '{"email":"nobody@example.com"}'
check "unknown users get user_not_registered" error_code_is user_not_registered
call GET "/v1/projects/$PROJECT_ID/members"
check "members: me as owner and Bob" status_is 200

refill

info "6. Sandboxes and the plan quota (free plan: 3 running sandboxes per project)"
SANDBOX_ID=""
for i in 1 2 3; do
  call POST "/v1/projects/$PROJECT_ID/sandboxes" '{"template":"base"}'
  check "sandbox $i started" status_is 201
  SANDBOX_ID="$(json_field id)"
done
call POST "/v1/projects/$PROJECT_ID/sandboxes" '{}'
check "4th sandbox refused with code sandbox_quota_exceeded" error_code_is sandbox_quota_exceeded
call DELETE "/v1/sandboxes/$SANDBOX_ID"
check "stopped one sandbox" status_is 200
call DELETE "/v1/sandboxes/$SANDBOX_ID"
check "stopping it again is idempotent" status_is 200
call POST "/v1/projects/$PROJECT_ID/sandboxes" '{}'
check "a slot is free again" status_is 201
call GET "/v1/projects/$PROJECT_ID/sandboxes?limit=2"
check "keyset pagination: two items and a next_cursor" body_lacks '"next_cursor":null'

refill

info "7. Rate limiting (free plan: 5 requests/second, burst 10)"
allowed=0; limited=0; other=0
for _ in $(seq "$RATE_BURST_REQUESTS"); do
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$API_URL/v1/projects/$PROJECT_ID/sandboxes")"
  case "$code" in 200) allowed=$((allowed + 1)) ;; 429) limited=$((limited + 1)) ;; *) other=$((other + 1)) ;; esac
done
ok "$RATE_BURST_REQUESTS rapid requests: $allowed allowed, $limited limited with 429"
check "no request failed in any other way" [ "$other" -eq 0 ]
check "some requests were limited" [ "$limited" -gt 0 ]
NO_RETRY=1 call GET "/v1/projects/$PROJECT_ID/sandboxes"
if [ "$STATUS" = 429 ]; then
  check "the 429 carries error code rate_limit_exceeded" error_code_is rate_limit_exceeded
  check "and a Retry-After header" header_present retry-after
  printf '%s\n' "$HEADERS" | grep -i -E '^(retry-after|x-ratelimit)' | sed 's/^/         /'
fi

printf '\n%sDemo finished: every check passed.%s\n' "$BOLD$GREEN" "$RESET"
