VERSION ?= dev

.PHONY: build test release

build:
	go build -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/tutti-network-probe .

test:
	go test ./...

release:
	@test "$(VERSION)" != "dev" || (echo "VERSION=vX.Y.Z is required" >&2; exit 2)
	scripts/release.sh "$(VERSION)"
