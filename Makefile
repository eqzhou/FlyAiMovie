.PHONY: dev-api dev-web build

dev-api:
	cd backend && go run ./cmd/server

dev-web:
	cd frontend && npm run dev

build:
	cd frontend && npm run build
	cd backend && go build -o ../flyaimovie ./cmd/server

.PHONY: sbom smoke-help
sbom:
	bash scripts/generate-sbom.sh artifacts/sbom

smoke-help:
	@echo 'Vendor smoke (skipped unless env keys set):'
	@echo '  cd backend && go test ./internal/services/adapters -run TestLiveSmoke -count=1'
	@echo 'SMTP smoke:'
	@echo '  cd backend && go test ./internal/httpapi -run TestLiveSmokeSMTP -count=1'
