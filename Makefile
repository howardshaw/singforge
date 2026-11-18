VERSION ?= dev
LDFLAGS := -s -w -X github.com/howardshaw/singforge/internal/cli.Version=$(VERSION)
.PHONY: build test check integration release scan
build:
	go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o bin/singforge ./cmd/singforge
test:
	go test -race ./...
check:
	test -z "$$(gofmt -l cmd internal)"
	go mod verify
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
	go test -race -count=1 ./...
scan:
	govulncheck ./...
integration: build
	python3 tests/integration.py --cli bin/singforge --core "$(CORE)" --mihomo "$(MIHOMO)" $(if $(CLOUDFLARED),--cloudflared "$(CLOUDFLARED)")
	python3 tests/menu.py --cli bin/singforge
	python3 tests/topologies.py --cli bin/singforge --core "$(CORE)" $(if $(CLOUDFLARED),--cloudflared "$(CLOUDFLARED)")
release:
	mkdir -p dist
	for arch in amd64 arm64; do CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o dist/singforge-linux-$$arch ./cmd/singforge || exit 1; done
	cd dist && shasum -a 256 singforge-linux-amd64 singforge-linux-arm64 > SHA256SUMS && shasum -a 256 -c SHA256SUMS
	cp LICENSE dist/LICENSE
	mkdir -p dist/licenses
	cp -f docs/licenses/* dist/licenses/

.PHONY: docs
docs: build
	python3 scripts/cli-reference.py bin/singforge
