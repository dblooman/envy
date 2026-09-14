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
	@test -r .envy/envy-dev/api-token || { echo "run make dev before make ui-dev" >&2; exit 1; }
	cd web && ENVY_API_TOKEN="$$(cat ../.envy/envy-dev/api-token)" pnpm dev

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
	$(MAKE) ui-build

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
