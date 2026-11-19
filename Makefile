.PHONY: build test vet race check boundary

build:
	go build -o seiner ./cmd/seiner

test:
	go test ./...

vet:
	go vet ./...

race:
	CGO_ENABLED=1 go test -race ./internal/gcp/... ./internal/app/... ./internal/ui/styles/... ./internal/ui/icons/...

boundary:
	@if go list -deps ./internal/app ./internal/ui/... | grep -Ei '^(cloud\.google\.com/|google\.golang\.org/|golang\.org/x/oauth2)'; then \
		echo "internal/app or internal/ui depends on a Google client library"; \
		exit 1; \
	fi

check: test vet boundary
