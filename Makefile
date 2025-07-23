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

generate-mock-all:
	mockgen -source=./internal/repository/repository.go -destination=./internal/repository/mocks/repository_mock.go -package=mocks