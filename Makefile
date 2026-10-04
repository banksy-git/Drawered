# Drawered build targets.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BIN := bin/drawered

.PHONY: all web build build-dev test check fmt fmt-check dev clean

all: web build

web/node_modules: web/package-lock.json
	cd web && npm ci
	touch $@

# Builds the Svelte app into internal/webui/dist, which the Go binary embeds.
web: web/node_modules
	cd web && npm run build
	touch internal/webui/dist/.gitkeep

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/drawered
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/drawered-import ./cmd/drawered-import

# A binary that honours DRAWERED_DEV_AUTH (bypasses OIDC). Never deploy it.
build-dev:
	go build -tags dev -ldflags "-X main.version=$(VERSION)-dev" -o bin/drawered-dev ./cmd/drawered

test:
	go test ./...

check: fmt-check test
	go vet ./...
	cd web && npm run check

fmt:
	./scripts/fmt.sh

fmt-check:
	./scripts/fmt.sh --check

# Runs the backend with development login on :8080. Run `npm run dev` in
# web/ alongside it for hot reloading on :5173.
dev: build-dev
	DRAWERED_BASE_URL=http://localhost:8080 DRAWERED_DATA_DIR=./data \
	DRAWERED_DEV_AUTH=$${DRAWERED_DEV_AUTH:-admin@example.com} \
	DRAWERED_BOOTSTRAP_ADMINS=$${DRAWERED_DEV_AUTH:-admin@example.com} \
	DRAWERED_INSECURE_COOKIES=true ./bin/drawered-dev serve

clean:
	rm -rf bin web/.svelte-kit
	find internal/webui/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
