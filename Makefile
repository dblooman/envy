.PHONY: dev routing-spike test test-e2e dev-down demo build
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

dev-down:
	bash deploy/local/down.sh

build:
	mkdir -p .envy/bin
	go build -o .envy/bin/delivery ./cmd/delivery
	go build -o .envy/bin/envy-mcp ./cmd/mcp
	go build -o .envy/bin/envy-server ./cmd/server
