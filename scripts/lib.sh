# Shared helpers for the scripts in this directory. Source it, do not run it.

# The same override the Makefile uses, for example COMPOSE="docker-compose".
COMPOSE="${COMPOSE:-docker compose}"

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  BOLD=$'\033[1m'; DIM=$'\033[2m'; RED=$'\033[31m'; GREEN=$'\033[32m'
  YELLOW=$'\033[33m'; BLUE=$'\033[34m'; CYAN=$'\033[36m'; RESET=$'\033[0m'
else
  BOLD=""; DIM=""; RED=""; GREEN=""; YELLOW=""; BLUE=""; CYAN=""; RESET=""
fi

say()  { printf '%s\n' "$*"; }
info() { printf '%s==>%s %s%s%s\n' "$BLUE" "$RESET" "$BOLD" "$*" "$RESET"; }
ok()   { printf '  %s✓%s %s\n' "$GREEN" "$RESET" "$*"; }
warn() { printf '  %s!%s %s\n' "$YELLOW" "$RESET" "$*" >&2; }
die()  { printf '%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }

# compact prints JSON from stdin on one line, cut to N characters (default 110).
compact() {
  local max="${1:-110}" text
  text="$(cat)"
  if command -v jq >/dev/null 2>&1; then text="$(printf '%s' "$text" | jq -cM . 2>/dev/null || printf '%s' "$text")"; fi
  if [ "${#text}" -gt "$max" ]; then text="${text:0:$max}…"; fi
  printf '%s' "$text"
}
