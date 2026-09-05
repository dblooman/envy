.PHONY: dev routing-spike test test-e2e dev-down demo
dev:
	bash deploy/local/bootstrap.sh
	bash deploy/local/build-demo.sh
	bash deploy/local/baseline.sh
	@if [ -f deploy/local/control-plane.sh ]; then bash deploy/local/control-plane.sh; fi

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
