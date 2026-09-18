# Developer entry points. Everything here is also runnable by hand; see
# docs/progress.md for the details.

# The dev database is the person's own rooms. Tests and end-to-end runs use
# the separate veyloom_test database so nothing they create lands there.
DATABASE_URL      ?= postgres://veyloom:veyloom@localhost:5432/veyloom?sslmode=disable
TEST_DATABASE_URL ?= postgres://veyloom:veyloom@localhost:5432/veyloom_test?sslmode=disable
SQLC_IMAGE        ?= sqlc/sqlc:1.30.0
PSQL               = docker compose exec -T postgres psql -U veyloom -d veyloom -v ON_ERROR_STOP=1

.PHONY: build build-go test test-db db-up db-down db-test db-reset db-backup sqlc web web-dev web-preview web-test web-format web-e2e

## build: compile the veyloom binary (the API) into bin/; the web client is built and served separately
build:
	go build -o bin/veyloom ./cmd/veyloom

## build-go: kept as an alias of build
build-go: build

## test: run the unit tests (database tests are skipped)
test:
	go test -race ./...

## test-db: run every test, including those that need Postgres (each test gets a throwaway database created via veyloom_test)
test-db: db-test
	VEYLOOM_TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test -race ./...

## db-up: start the local Postgres from docker-compose.yml
db-up:
	docker compose up -d

## db-down: stop the local Postgres (data volume is kept)
db-down:
	docker compose down

## db-test: make sure the veyloom_test database exists (a fresh volume gets it from docker/postgres-init)
db-test:
	@$(PSQL) -Atc "SELECT 1 FROM pg_database WHERE datname = 'veyloom_test'" | grep -q 1 || docker compose exec -T postgres createdb -U veyloom veyloom_test

## db-reset: wipe every project, room, message, turn, approval, attachment, agent, member and user in the DEV database; machines and the account (it lives in <state-dir>/account.json) stay. The schema stays too: after a migration was edited in place, drop and recreate the database instead. Take a backup first: make db-backup
db-reset:
	$(PSQL) -c "BEGIN; TRUNCATE TABLE attachments, approvals, turns, messages, threads, members, agents, rooms, projects, users RESTART IDENTITY CASCADE; COMMIT;"
	@echo "Dev database wiped. Transcripts and attachments on disk live under the state dir (default ~/.veyloom/turns and ~/.veyloom/attachments); remove them by hand if wanted."

## db-backup: dump the dev database to ~/.veyloom/backups
db-backup:
	@mkdir -p ~/.veyloom/backups
	docker compose exec -T postgres pg_dump -U veyloom -d veyloom --no-owner > ~/.veyloom/backups/veyloom-$$(date +%Y%m%d-%H%M%S).sql
	@ls -1t ~/.veyloom/backups | head -1

## sqlc: regenerate internal/store/db from the SQL under internal/store
sqlc:
	docker run --rm -v "$(CURDIR):/src" -w /src $(SQLC_IMAGE) generate

# npm ci runs only when the lockfile changes; the stamp file is npm's own.
web/node_modules/.package-lock.json: web/package-lock.json
	cd web && npm ci

## web: build the web client into web/dist (needs Node 22+); serve it with web-preview or any static host that proxies /api
web: web/node_modules/.package-lock.json
	cd web && npm run build

## web-dev: run the Vite dev server on :7789, proxying /api to a running `veyloom serve` (VEYLOOM_API to point elsewhere)
web-dev: web/node_modules/.package-lock.json
	cd web && npm run dev

## web-preview: serve the built web/dist on :7790 with the same /api proxy
web-preview: web
	cd web && npm run preview

## web-test: lint (ESLint, Prettier), type-check and unit-test the web UI
web-test: web/node_modules/.package-lock.json
	cd web && npm run lint && npx vitest run

## web-format: rewrite the web UI's files in its Prettier style; web-test fails on unformatted ones
web-format: web/node_modules/.package-lock.json
	cd web && npm run format

## web-e2e: build the binary and the client, then run Playwright: the API on the veyloom_test database, the client from vite preview
web-e2e: build web db-test
	cd web && E2E_DATABASE_URL="$(TEST_DATABASE_URL)" npx playwright test
