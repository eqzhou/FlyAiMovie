.PHONY: dev-api dev-web build sbom smoke-help supply-chain-test oci image-sbom

dev-api:
	cd backend && go run ./cmd/server

dev-web:
	cd frontend && npm run dev

build:
	cd frontend && npm run build
	cd backend && go build -o ../flyaimovie ./cmd/server

sbom:
	bash scripts/generate-sbom.sh artifacts/sbom

supply-chain-test:
	bash scripts/test-supply-chain-config.sh

oci:
	bash scripts/build-oci.sh artifacts/flyaimovie-oci.tar

image-sbom:
	bash scripts/generate-image-sbom.sh artifacts/flyaimovie-oci.tar artifacts/sbom

smoke-help:
	@echo 'Vendor smoke (skipped unless env keys set):'
	@echo '  cd backend && go test ./internal/services/adapters -run TestLiveSmoke -count=1'
	@echo 'SMTP smoke:'
	@echo '  cd backend && go test ./internal/httpapi -run TestLiveSmokeSMTP -count=1'
