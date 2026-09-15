.DEFAULT_GOAL := help

.PHONY: dev routing-spike test test-e2e test-helm test-live-gatewayapi dev-down demo build ui-dev ui-build sqlc generate helm-lint
dev:
	bash deploy/local/bootstrap.sh
	bash deploy/local/build-demo.sh
	bash deploy/local/baseline.sh
	bash deploy/local/control-plane.sh

routing-spike:
	bash deploy/local/bootstrap.sh
	bash deploy/local/build-demo.sh
	bash deploy/local/baseline.sh
	bash deploy/local/spike.sh

test:
	go test ./...

test-e2e:
	bash deploy/local/e2e.sh

test-helm:
	bash deploy/local/helm-smoke.sh

test-live-gatewayapi:
	ENVY_TEST_GATEWAY_API_RESOURCES=1 go test -v -count=1 -run TestGatewayAPIResources ./tests/installation/...

dev-down:
	bash deploy/local/down.sh

build:
	mkdir -p .envy/bin
	go build -o .envy/bin/delivery ./cmd/delivery
	go build -o .envy/bin/envy-mcp ./cmd/mcp
	go build -o .envy/bin/envy-server ./cmd/server

sqlc:
	sqlc generate

generate: sqlc

ui-dev:
	cd web && pnpm dev

ui-build:
	cd web && pnpm build

helm-lint:
	@command -v helm >/dev/null || { echo "helm is required; install Helm 3.19.0 or newer" >&2; exit 1; }
	helm lint deploy/helm/envy --set installationID=lint-install --set externalDatabase.secretName=lint-db --set externalDatabase.secretKey=url
	helm template envy deploy/helm/envy --namespace envy-system --set installationID=lint-install --set externalDatabase.secretName=lint-db --set externalDatabase.secretKey=url >/dev/null
	helm template envy deploy/helm/envy --namespace envy-system --set installationID=lint-install --set externalDatabase.secretName=lint-db --set externalDatabase.secretKey=url --set preflight.enabled=true >/dev/null

.PHONY: dev-shop
dev-shop:
	bash deploy/local/shop.sh

.PHONY: test-frontend
test-frontend:
	node --test integrations/cloudflare-pages/build.test.mjs
	go test ./internal/domain ./internal/client ./internal/cli ./internal/mcp ./internal/persistence/postgres ./examples/shop
	$(MAKE) ui-check

.PHONY: test-lan lan-acceptance
test-lan:
	python3 -m unittest discover -s deploy/lan -p '*_test.py'

lan-acceptance:
	python3 deploy/lan/acceptance.py

.PHONY: test-derived-e2e
test-derived-e2e:
	bash deploy/local/derived-e2e.sh
.PHONY: test-mesh test-mesh-charts
test-mesh:
	bash deploy/testing/e2e.sh $(MESH)
test-mesh-charts:
	python3 deploy/testing/check-charts.py

.PHONY: help setup doctor check check-go check-integrations ui-check site-dev site-check format-check lint
help:
	@echo "make setup              Install Go, dashboard, and site dependencies"
	@echo "make doctor             Check tools, Docker, platform, and local ports"
	@echo "make check              Run all fast checks (no cluster required)"
	@echo "make ui-check/site-check Validate one frontend"
	@echo "make ui-dev             Dashboard with local API credentials (after make dev)"
	@echo "make site-dev           Documentation at http://localhost:4321/envy/"
	@echo "make dev / dev-down     Start / delete the local envy-dev cluster"
	@echo "make build              Build delivery, MCP, and server binaries"
	@echo "make test-e2e           Disposable-cluster lifecycle acceptance"
	@echo "make test-mesh MESH=istio  Full mesh acceptance (also: cilium)"

setup:
	go mod download
	cd web && pnpm install --frozen-lockfile
	cd site && pnpm install --frozen-lockfile

doctor:
	@command -v python3 >/dev/null || { echo "Install Python 3 to run make doctor" >&2; exit 1; }
	python3 scripts/doctor.py

check: check-go ui-check site-check check-integrations helm-lint test-mesh-charts

check-go:
	go mod tidy -diff
	$(MAKE) lint
	go test ./...

lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint v2.13.2 is required; install it with: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2" >&2; exit 1; }
	golangci-lint run ./...

ui-check:
	cd web && pnpm check

site-dev:
	cd site && pnpm dev

site-check:
	cd site && pnpm check

format-check:
	cd web && pnpm format:check
	cd site && pnpm format:check

check-integrations:
	python3 -m unittest discover -s scripts -p 'test_*.py'
	python3 -m unittest discover -s integrations/github-actions -p 'test_*.py'
	python3 -m unittest discover -s integrations/local-preview -p 'test_*.py'
	python3 -m unittest discover -s deploy/lan -p '*_test.py'
	node --test integrations/cloudflare-pages/build.test.mjs
	python3 deploy/testing/render-versions.py --check
