# syntax=docker/dockerfile:1.7

# Frontend build
FROM node:20-slim@sha256:2cf067cfed83d5ea958367df9f966191a942351a2df77d6f0193e162b5febfc0 AS frontend-build
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm,sharing=locked \
    npm ci
COPY frontend/ ./
RUN npm run build

# Go build
FROM golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS backend-build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=1 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath -ldflags='-s -w' -o /out/flyaimovie ./cmd/server

# Runtime
FROM debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl ffmpeg \
    && groupadd --system --gid 10001 flyaimovie \
    && useradd --system --uid 10001 --gid flyaimovie --home-dir /app --shell /usr/sbin/nologin flyaimovie
WORKDIR /app
COPY --from=backend-build /out/flyaimovie /app/flyaimovie
COPY --from=frontend-build /app/frontend/dist /app/frontend/dist
COPY backend/skills /app/backend/skills
COPY configs/config.example.yaml /app/configs/config.yaml
RUN install -d -o flyaimovie -g flyaimovie /app/data /app/data/storage
ENV PORT=5679
EXPOSE 5679
VOLUME ["/app/data"]
USER 10001:10001
CMD ["/app/flyaimovie"]
