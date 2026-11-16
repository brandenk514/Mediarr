# syntax=docker/dockerfile:1
#
# Multi-stage build: full Go toolchain in "build", Node 22 toolchain in
# "ui-build", and the static binary plus the built UI on distroless in
# "runtime". The runtime image has NO shell, NO package manager, and no
# writable filesystem outside explicitly declared volumes.

ARG GO_VERSION=1.27.4
ARG NODE_VERSION=22

# ---- build -----------------------------------------------------------------
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Build context: source only (Dockerfile is the context root).
COPY . .

# Writable scratch dirs, pre-created here so the runtime COPY can chown them.
# Named volumes seeded from these directories inherit the ownership, letting
# the nonroot app user (65532) write its downloads and imported media.
RUN mkdir -p /writables/downloads /writables/media

# Reproducible, static binary. Version stamped from CI / local git.
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w -buildid= \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.buildTime=${BUILD_DATE}" \
      -o /out/mediarr ./cmd/mediarr

# ---- ui-build ----------------------------------------------------------------
# The UI is built from source inside the image pipeline: `npm ci` installs
# strictly from the committed ui/package-lock.json (single source of npm
# dependencies — no hand edits, no drift) and `npm run build` (tsc -b &&
# vite build) produces the complete dist. Only the finished dist crosses
# into the runtime stage: node, npm, node_modules, and the rest of the
# toolchain stay in this stage. node:22-bookworm matches the `ui` CI job's
# toolchain (Node 22, .github/workflows/ci.yml) and the golang bookworm
# family.
FROM node:${NODE_VERSION}-bookworm AS ui-build
WORKDIR /app

# Lockfile and ui/.npmrc first (its legacy-peer-deps setting npm ci must
# honor), for layer caching.
COPY ui/package.json ui/package-lock.json ui/.npmrc ./
RUN npm ci --no-audit --no-fund

COPY ui/ .
RUN npm run build

# ---- runtime ---------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# nonroot user (uid/gid 65532) is baked into the base image; the nonroot
# variant already runs as that user.
COPY --from=build /out/mediarr /mediarr

# Writable scratch for the mock download client (/downloads) and imported
# media (/media). These are pre-created and chowned to the app user in the
# build stage; seeding the named volumes from these image paths makes the
# mounted volumes inherit that ownership so the nonroot user can write them.
COPY --from=build --chown=nonroot:nonroot --chmod=775 /writables/downloads /downloads
COPY --from=build --chown=nonroot:nonroot --chmod=775 /writables/media /media

# The complete built UI (index.html + hashed assets + public/ files), read-only
# on the rootfs and owned by the app user so the nonroot runtime can read it.
# Served from this path by #87; this task only ships the asset placement.
COPY --from=ui-build --chown=nonroot:nonroot /app/dist /home/nonroot/ui

EXPOSE 8080

# No shell in distroless — the binary itself is the healthcheck. It pings
# Postgres + Redis and exits 0/1.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/mediarr", "healthcheck"]

ENTRYPOINT ["/mediarr"]
