# Makefile for simplifying common tasks

# --- .env loading ---
# Check if .env file exists
ifneq (,$(wildcard ./.env))
    # Include .env file and export its variables to the environment
    include .env
    export
endif
# --- End .env loading ---

# Variables
DB_URL=$(NEON_DATABASE_URL)
# UPDATED: Remove the 'file://' prefix. The migrate CLI will add it.
MIGRATIONS_PATH=db/migrations

# This is the Go binary we build
BINARY_NAME=foodwright-api

.PHONY: help run build migrateup migratedown

help:
	@echo "Commands:"
	@echo "  run          - Runs the API server (and loads .env)"
	@echo "  build        - Builds the Go binary"
	@echo "  migrateup    - Applies all 'up' migrations to the database"
	@echo "  migratedown  - Reverts the last 'down' migration"

build:
	@echo "Building binary..."
	@go build -o bin/$(BINARY_NAME) ./cmd/api

run: build
	@echo "Starting server..."
	@./bin/$(BINARY_NAME)

# DANGER: 'force' is needed if migrations get stuck. Use with caution.
# migrateforce:
# 	@echo "Forcing migration version..."
# 	@migrate -database "$(DB_URL)" -path "$(MIGRATIONS_PATH)" force 1

migrateup:
	@echo "Running migrations 'up'..."
	@echo "Connecting to: $(DB_URL)" | sed 's,@[^/]*:,@********:,g' # Obfuscate password in log
	@migrate -database "$(DB_URL)" -path "$(MIGRATIONS_PATH)" up

migratedown:
	@echo "Running migrations 'down'..."
	@echo "Connecting to: $(DB_URL)" | sed 's,@[^/]*:,@********:,g' # Obfuscate password in log
	@migrate -database "$(DB_URL)" -path "$(MIGRATIONS_PATH)" down 1