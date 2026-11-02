# syntax=docker/dockerfile:1
# Build from the repository root: shared/logging is a local Go module replacement.
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build

ARG SERVICE
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src
COPY . .

RUN set -eu; \
    case "$SERVICE" in config-center|config-center-access|sdk-core) ;; *) echo "invalid SERVICE: $SERVICE" >&2; exit 1;; esac; \
    cd "$SERVICE"; \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
      go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/server "./cmd/$SERVICE"; \
    if [ "$SERVICE" = "config-center" ]; then \
      CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
        go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate; \
    fi

FROM alpine:3.23
RUN apk add --no-cache ca-certificates \
    && addgroup -S -g 65532 app \
    && adduser -S -D -H -u 65532 -G app app

WORKDIR /app
COPY --from=build --chown=65532:65532 /out/ /app/
USER 65532:65532

ENTRYPOINT ["/app/server"]
