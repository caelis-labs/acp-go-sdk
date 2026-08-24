GOCACHE ?= $(CURDIR)/.gocache
GOMODCACHE ?= $(CURDIR)/.gomodcache
INTEROP_EVIDENCE_DIR ?= $(CURDIR)/.artifacts/interop
GENERATED := agent_gen.go client_gen.go constants_gen.go helpers_gen.go types_gen.go

.PHONY: verify-schema
verify-schema:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run ./cmd/schemaverify -schema ./schema

.PHONY: generate
generate: verify-schema
	cd cmd/generate && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run .

.PHONY: check-generated
check-generated: verify-schema
	@tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' EXIT; \
	cd cmd/generate && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go run . -schema ../../schema -out "$$tmp"; \
	cd ../..; \
	status=0; \
	for file in $(GENERATED); do diff -u "$$file" "$$tmp/$$file" || status=1; done; \
	exit $$status

.PHONY: test
test:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...
	cd cmd/generate && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test ./...

.PHONY: test-race
test-race:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test -race ./...
	cd cmd/generate && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test -race ./...

.PHONY: vet
vet:
	GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...
	cd cmd/generate && GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go vet ./...

.PHONY: check
check: verify-schema check-generated test vet
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.gocache/*' -not -path './.gomodcache/*'))"
	git diff --check

.PHONY: interop-prepare
interop-prepare:
	cd interop/peers/typescript && npm ci --ignore-scripts && npm run build
	cd interop/peers/rust && cargo build --locked

.PHONY: interop-check
interop-check: interop-prepare
	cd interop/peers/rust && cargo fmt --check
	cd interop/peers/rust && cargo clippy --locked --all-targets -- -D warnings
	cd interop/peers/rust && cargo test --locked

.PHONY: interop
interop: interop-check
	mkdir -p $(INTEROP_EVIDENCE_DIR)
	ACP_INTEROP=1 ACP_INTEROP_EVIDENCE_DIR=$(INTEROP_EVIDENCE_DIR) GOCACHE=$(GOCACHE) GOMODCACHE=$(GOMODCACHE) go test -count=1 -timeout=3m ./interop
