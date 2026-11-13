.PHONY: dev test build css clean-data

dev: ; go run ./cmd/server
test: ; go test ./...
build: ; go build -o study-words ./cmd/server
