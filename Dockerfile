# Frontend build
FROM node:20-slim AS frontend-build
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Go build
FROM golang:1.26-bookworm AS backend-build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=1 go build -o /out/flyaimovie ./cmd/server

# Runtime
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg \
  && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=backend-build /out/flyaimovie /app/flyaimovie
COPY --from=frontend-build /app/frontend/dist /app/frontend/dist
COPY backend/skills /app/backend/skills
COPY configs/config.example.yaml /app/configs/config.yaml
RUN mkdir -p /app/data/storage
ENV PORT=5679
EXPOSE 5679
VOLUME ["/app/data"]
CMD ["/app/flyaimovie"]
