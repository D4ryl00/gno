rundep := go run -modfile ../../misc/devdeps/go.mod
golangci_lint := $(rundep) github.com/golangci/golangci-lint/v2/cmd/golangci-lint

# Reference magic-wormhole mailbox server and client, for the wormhole tests.
venv := .venv

.PHONY: install
install:
	go install ./...

.PHONY: lint
lint:
	$(golangci_lint) --config ../../.github/golangci.yml run ./...

# Without test-deps, the wormhole tests that need a relay skip.
.PHONY: test
test:
	go test $(GOTEST_FLAGS) ./...

.PHONY: test-deps
test-deps: $(venv)

# Fails instead of skipping without the venv; run by
# .github/workflows/ci-gnokey-pair-relay.yml.
.PHONY: test-relay
test-relay: $(venv)
	WORMHOLE_REQUIRE_RELAY=1 go test $(GOTEST_FLAGS) -race ./...

$(venv): relaytest/requirements.txt
	python3 -m venv $(venv)
	$(venv)/bin/pip install --quiet --requirement relaytest/requirements.txt
	touch $(venv)
