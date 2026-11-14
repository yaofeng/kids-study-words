# syntax=docker/dockerfile:1
# The go.mod directive needs a 1.26 toolchain (the spec's "Go 1.24+" is a floor).
FROM golang:1.26-alpine AS build
RUN apk add --no-cache ca-certificates curl bash
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN bash scripts/prepare-dict.sh
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w" \
    -o /out/study-words ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 1000 app \
    && adduser -D -u 1000 -G app app \
    && mkdir -p /data \
    && chown app:app /data

COPY --from=build /out/study-words /app/study-words
COPY --from=build /out/dict.db /app/dict.db

ENV DATA_DIR=/data \
    DICT_PATH=/app/dict.db \
    PORT=8080

USER app
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/app/study-words"]
