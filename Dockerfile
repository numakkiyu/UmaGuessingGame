# Build the SPA and Go binaries, then run without the build toolchain
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot

FROM node:24-bookworm-slim AS web

WORKDIR /web

# Install dependencies from the lockfile only, so the layer cache survives
# source edits.
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile

COPY web/ ./
# Type-checking includes the shared rules fixture used by the practice tests.
# Preserve its repository-relative location; do not copy unrelated datasets.
COPY data/manual/rules-track.json /data/manual/rules-track.json
RUN pnpm run build


FROM golang:1.26-bookworm AS server

WORKDIR /src
ARG GO_MODULE_PROXY=https://proxy.golang.org,direct

# Module download is cached separately from the source tree.
COPY go.mod go.sum ./
RUN GOPROXY=${GO_MODULE_PROXY} go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
# Replace the embed directory because Docker COPY merges existing files
RUN rm -rf ./internal/spafs/dist
COPY --from=web /web/dist ./internal/spafs/dist/

# CGO is not needed: modernc.org/sqlite is a pure Go driver.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uma-server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/uma-content ./cmd/content

# Prepare writable volume roots while shell utilities are available. The final
# distroless stage has no shell, mkdir or chown.
RUN mkdir -p /runtime/var/lib/umaguess /runtime/backups


FROM ${RUNTIME_IMAGE}

# /app        read-only  binary + embedded assets
# /content    read-only  versioned packs, manifest, rules
# /var/lib/umaguess writable local volume: runtime.db, WAL, SHM
# /backups    separate controlled path for consistent backups
COPY --from=server /out/uma-server /app/uma-server
COPY --from=server /out/uma-content /app/uma-content
# The Debian slim alternative does not ship a CA bundle. Both supported runtimes
# use this bundle from the trusted Go build image for outbound TLS validation.
COPY --from=server /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Empty named volumes inherit ownership from these directories
COPY --from=server --chown=65532:65532 /runtime/ /

# Non-root by default; the distroless :nonroot tag supplies uid/gid 65532.
USER 65532:65532

ENV UMA_ADDR=:8080 \
    UMA_DATA_DIR=/var/lib/umaguess \
    UMA_CONTENT_DIR=/content \
    UMA_BACKUP_DIR=/backups

EXPOSE 8080

# Health is served by the application itself so the image needs no shell tools.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/app/uma-server", "healthcheck"]

ENTRYPOINT ["/app/uma-server"]
