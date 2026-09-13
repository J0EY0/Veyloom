# Developer entry points. Everything here is also runnable by hand; see
# docs/progress.md for the details.

DATABASE_URL ?= postgres://veyloom:veyloom@localhost:5432/veyloom?sslmode=disable
SQLC_IMAGE   ?= sqlc/sqlc:1.30.0

.PHONY: build test test-db db-up db-down sqlc

## build: compile the veyloom binary into bin/
build:
	go build -o bin/veyloom ./cmd/veyloom

## test: run the unit tests (database tests are skipped)
test:
	go test -race ./...

## test-db: run every test, including those that need Postgres
test-db:
	VEYLOOM_TEST_DATABASE_URL="$(DATABASE_URL)" go test -race ./...

## db-up: start the local Postgres from docker-compose.yml
db-up:
	docker compose up -d

## db-down: stop the local Postgres (data volume is kept)
db-down:
	docker compose down

## sqlc: regenerate internal/store/db from the SQL under internal/store
sqlc:
	docker run --rm -v "$(CURDIR):/src" -w /src $(SQLC_IMAGE) generate
