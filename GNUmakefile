default: build

build:
	go build -o terraform-provider-costfluent ./cmd/terraform-provider-costfluent

install: build
	mkdir -p ~/.terraform.d/plugins/local/costfluent/costfluent/0.1.0/darwin_arm64
	mv terraform-provider-costfluent ~/.terraform.d/plugins/local/costfluent/costfluent/0.1.0/darwin_arm64/

test:
	go test -v ./...

testacc:
	TF_ACC=1 go test -v ./tests/acceptance/... -timeout 30m

# Pinned rather than floating: the generated pages are committed and published to the Terraform
# Registry, and a newer generator rewrites all of them on whoever runs it next.
TFPLUGINDOCS_VERSION := v0.25.0

docs:
	TFPLUGINDOCS_VERSION=$(TFPLUGINDOCS_VERSION) scripts/generate-docs.sh

# The Registry publishes docs/ as committed, so a schema change without `make docs` ships stale pages.
docs-check: docs
	@test -z "$$(git status --porcelain -- docs)" || { git status --porcelain -- docs; echo 'docs/ is stale: run make docs and commit the result' >&2; exit 1; }

lint:
	golangci-lint run

fmt:
	go fmt ./...
	gofumpt -w .

tidy:
	go mod tidy

.PHONY: build install test testacc docs docs-check lint fmt tidy
