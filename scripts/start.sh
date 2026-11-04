#!/usr/bin/env bash
# One command to run everything: builds the image, starts the stack, applies
# migrations and waits until every service is healthy.
set -euo pipefail
cd "$(dirname "$0")/.."
. scripts/lib.sh

PORTS="5432 6379 8080 4444 4445 3000"
LOG="$(mktemp)"
trap 'rm -f "$LOG"' EXIT

command -v docker >/dev/null || die "docker is required"
docker info >/dev/null 2>&1 || die "the docker daemon is not running"

# If our own stack is not running, a busy port belongs to something else.
if [ -z "$($COMPOSE ps -q 2>/dev/null)" ] && command -v lsof >/dev/null; then
  for port in $PORTS; do
    if owner="$(lsof -nP -iTCP:"$port" -sTCP:LISTEN 2>/dev/null | awk 'NR==2 {print $1 " (pid " $2 ")"}')" && [ -n "$owner" ]; then
      die "port $port is already in use by $owner. Stop it, then run make start again."
    fi
  done
fi

info "Starting PostgreSQL, Redis, Ory Hydra and the API"
say "  ${DIM}the first run builds the image and pulls images. Hydra's login UI is emulated on Apple Silicon and boots slowly${RESET}"
started="$(date +%s)"
if ! $COMPOSE up -d --build --wait >"$LOG" 2>&1; then
  tail -n 25 "$LOG" >&2
  $COMPOSE ps >&2
  die "the stack did not become healthy. See: $COMPOSE logs"
fi
ok "all services healthy in $(( $(date +%s) - started ))s"

info "Migrations"
$COMPOSE logs --no-log-prefix migrate 2>/dev/null | grep '^migrations:' | tail -n 3 | sed 's/^/  /' || true

info "Services"
$COMPOSE ps --format '{{.Service}}|{{.Status}}' | sort | awk -F'|' '{ printf "  %-22s %s\n", $1, $2 }'

# Give a slow dependency a few seconds to settle before reporting it.
ready=""
for _ in 1 2 3 4 5; do
  ready="$(curl -s --max-time 3 http://localhost:8080/readyz || true)"
  case "$ready" in *'"status":"ok"'*) break ;; esac
  sleep 2
done
printf '\n  readiness: %s\n' "${ready:-unreachable}"

cat <<MSG

${BOLD}E2B Sandbox API is up${RESET}

  API         ${CYAN}http://localhost:8080${RESET}
  Sign in     ${CYAN}http://localhost:8080/auth/login${RESET}   ${DIM}(demo user: foo@bar.com / foobar)${RESET}
  Hydra       http://localhost:4444   login UI: http://localhost:3000

Next:
  ${BOLD}make demo${RESET}      scripted walk through the API with real requests
  ${BOLD}make fixtures${RESET}  load demo data (also done by make demo)
  ${BOLD}make login${RESET}     print an access token for curl
  ${BOLD}make logs${RESET}      follow API logs
  ${BOLD}make stop${RESET}      stop the stack
MSG
