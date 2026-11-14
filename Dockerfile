# syntax=docker/dockerfile:1
# The go.mod directive needs a 1.26 toolchain (the spec's "Go 1.24+" is a floor).
#
# Both target architectures are cross-compiled on the build platform. CGO is
# disabled, so no QEMU is needed for the Go build. The dictionary is
# architecture-independent and is built once here, then shared by both images.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates curl bash
WORKDIR /src

# Cache the pinned dictionary sources independently of the app source.
COPY scripts/fetch-data.sh ./scripts/fetch-data.sh
RUN bash scripts/fetch-data.sh

COPY go.mod go.sum ./
RUN go mod download

# Only the dictionary builder's dependency closure goes in this layer, so
# app-only changes do not rebuild dict.db.
COPY cmd/dictbuild ./cmd/dictbuild
COPY internal/dictbuild ./internal/dictbuild
RUN go run ./cmd/dictbuild \
      -ecdict data-src/ecdict.csv \
      -sentences data-src/cmn-eng.zip \
      -out /out/dict.db

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
      -ldflags "-s -w" -o /out/study-words-amd64 ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath \
      -ldflags "-s -w" -o /out/study-words-arm64 ./cmd/server

FROM alpine:3.20
ARG TARGETARCH=amd64
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 1000 app \
    && adduser -D -u 1000 -G app app \
    && mkdir -p /data \
    && chown app:app /data

COPY --from=build /out/study-words-${TARGETARCH} /app/study-words
COPY --from=build /out/dict.db /app/dict.db

ENV DATA_DIR=/data \
    DICT_PATH=/app/dict.db \
    PORT=8080

USER app
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/app/study-words"]
