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

generate:
	go generate ./...

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

lint:
	golangci-lint run

fmt:
	go fmt ./...
	gofumpt -w .

tidy:
	go mod tidy

sync-sdk:
	scripts/sync-sdk.sh

.PHONY: build install test testacc generate docs lint fmt tidy sync-sdk
