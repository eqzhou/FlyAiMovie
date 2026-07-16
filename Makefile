.PHONY: dev-api dev-web build

dev-api:
	cd backend && go run ./cmd/server

dev-web:
	cd frontend && npm run dev

build:
	cd frontend && npm run build
	cd backend && go build -o ../flyaimovie ./cmd/server
