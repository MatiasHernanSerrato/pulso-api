.PHONY: help db-up db-down fmt lint test run graphql

help:
	@echo "targets:"
	@echo "  db-up    - levanta postgres + timescaledb"
	@echo "  db-down  - baja la db"
	@echo "  fmt      - formatea codigo"
	@echo "  lint     - corre linter"
	@echo "  test     - corre tests"
	@echo "  run      - corre el server"
	@echo "  graphql  - regenera resolvers y models de gqlgen"

graphql:
	go run github.com/99designs/gqlgen generate

db-up:
	docker compose up -d db

db-down:
	docker compose down

fmt:
	gofmt -w .
	goimports -w .

lint:
	golangci-lint run ./...

test:
	go test -race -count=1 ./...

run:
	go run ./cmd/server
