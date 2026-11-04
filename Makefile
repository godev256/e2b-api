.DEFAULT_GOAL := help
COMPOSE ?= docker compose
export COMPOSE
API_URL ?= http://localhost:8080

##@ Run

.PHONY: start
start: ## Build and start the whole stack, apply migrations, print how to use it
	@./scripts/start.sh

.PHONY: fixtures
fixtures: ## Load demo data (users, projects, sandboxes). Safe to repeat
	@$(COMPOSE) run --rm --no-deps migrate fixtures

.PHONY: demo
demo: ## Walk through the API with real requests: auth, sharing, quota, rate limit
	@API_URL=$(API_URL) ./scripts/demo.sh

.PHONY: login
login: ## Print an access token for the demo user (foo@bar.com)
	@API_URL=$(API_URL) ./scripts/login.sh

.PHONY: logs
logs: ## Follow the API logs
	@$(COMPOSE) logs -f api

.PHONY: stop
stop: ## Stop the stack, keep the data
	@$(COMPOSE) down

.PHONY: reset
reset: ## Stop the stack and delete the database volume (asks first, FORCE=1 skips)
	@if [ -t 0 ] && [ "$(FORCE)" != 1 ]; then \
		printf 'This deletes the database volume. Continue? [y/N] '; read answer; \
		[ "$$answer" = y ] || { echo aborted; exit 1; }; \
	fi
	@$(COMPOSE) down -v

##@ Develop

.PHONY: sqlc
sqlc: ## Regenerate the sqlc code
	go tool sqlc generate

.PHONY: test
test: ## Unit tests
	go test -race ./...

.PHONY: test-integration
test-integration: ## Integration tests against the running stack (make start first)
	go test -race -tags integration -count=1 ./internal/integration/...

.PHONY: lint
lint: ## Static analysis
	golangci-lint run ./...

.PHONY: check
check: sqlc lint test ## Regenerate sqlc, lint, run unit tests

##@ Help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)
