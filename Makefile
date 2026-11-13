.PHONY: dev test build css clean-data fetch-data dict

dev: ; go run ./cmd/server
test: ; go test ./...
build: ; go build -o study-words ./cmd/server
css: ; npx --yes tailwindcss@3.4.17 -i web/static/input.css -o web/static/app.css --minify
fetch-data: ; bash scripts/fetch-data.sh
dict: fetch-data ; go run ./cmd/dictbuild -ecdict data-src/ecdict.csv -sentences data-src/cmn-eng.zip -out data/dict.db
clean-data: ; rm -rf data-src
