SQLC_VERSION := v1.29.0

.PHONY: generate test vet
generate:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

test:
	go test -race ./...

vet:
	go vet ./...
