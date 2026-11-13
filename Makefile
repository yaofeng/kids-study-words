.PHONY: dev test build css clean-data

dev: ; go run ./cmd/server
test: ; go test ./...
build: ; go build -o study-words ./cmd/server
css: ; npx --yes tailwindcss@3.4.17 -i web/static/input.css -o web/static/app.css --minify
