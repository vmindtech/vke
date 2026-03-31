.PHONY: migrate-up migrate-down migrate-force

# Usage:
#   make migrate-up DATABASE_URL='mysql://user:pass@tcp(host:3306)/db?charset=utf8&parseTime=true&loc=UTC'
#
# For CI/CD: run migrate-up before deploying API/worker.

MIGRATE_IMAGE ?= migrate/migrate:v4.17.1
MIGRATIONS_DIR ?= $(CURDIR)/migrations

migrate-up:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required"; exit 1)
	docker run --rm \
	  -v "$(MIGRATIONS_DIR)":/migrations \
	  "$(MIGRATE_IMAGE)" \
	  -path=/migrations \
	  -database "$(DATABASE_URL)" \
	  up

migrate-down:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required"; exit 1)
	docker run --rm \
	  -v "$(MIGRATIONS_DIR)":/migrations \
	  "$(MIGRATE_IMAGE)" \
	  -path=/migrations \
	  -database "$(DATABASE_URL)" \
	  down 1

# Dangerous: use only if you know what you're doing
migrate-force:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is required"; exit 1)
	@test -n "$(VERSION)" || (echo "VERSION is required"; exit 1)
	docker run --rm \
	  -v "$(MIGRATIONS_DIR)":/migrations \
	  "$(MIGRATE_IMAGE)" \
	  -path=/migrations \
	  -database "$(DATABASE_URL)" \
	  force "$(VERSION)"

lint:
	golangci-lint run

test-unit:
	go test ./internal/... -race -coverprofile=coverage.out -covermode=atomic -v

test-repository:
	go test ./internal/repository... -race -coverprofile=coverage.out -covermode=atomic -v

test-integration:
	go test -tags integration ./internal/handler/... -race -coverprofile=coverage_integration.out -coverpkg=./internal/handler/... -covermode=atomic -v

doc:
	swag init --parseDependency -g internal/route/route.go -o docs

db-migration:
	go run ./cmd/cli/db_migration.go

db-add-errors-table:
	@echo "Adding errors table to database..."
	@read -p "Enter MySQL host: " MYSQL_HOST; \
	read -p "Enter MySQL user: " MYSQL_USER; \
	read -p "Enter MySQL password: " MYSQL_PASS; \
	read -p "Enter database name: " DB_NAME; \
	mysql -h $$MYSQL_HOST -u $$MYSQL_USER --password=$$MYSQL_PASS --database=$$DB_NAME < scripts/add_errors_table.sql

db-add-resources-table:
	@echo "Adding resources table to database..."
	@read -p "Enter MySQL host: " MYSQL_HOST; \
	read -p "Enter MySQL user: " MYSQL_USER; \
	read -p "Enter MySQL password: " MYSQL_PASS; \
	read -p "Enter database name: " DB_NAME; \
	mysql -h $$MYSQL_HOST -u $$MYSQL_USER --password=$$MYSQL_PASS --database=$$DB_NAME < scripts/add_resources_table.sql

db-add-node-groups-taints:
	@echo "Adding taint support to node_groups table..."
	@read -p "Enter MySQL host: " MYSQL_HOST; \
	read -p "Enter MySQL user: " MYSQL_USER; \
	read -p "Enter MySQL password: " MYSQL_PASS; \
	read -p "Enter database name: " DB_NAME; \
	mysql -h $$MYSQL_HOST -u $$MYSQL_USER --password=$$MYSQL_PASS --database=$$DB_NAME < scripts/add_node_groups_taints.sql

generate-mock-all:
	mockgen -source=./internal/repository/repository.go -destination=./internal/repository/mocks/repository_mock.go -package=mocks