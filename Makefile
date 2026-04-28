.PHONY: build test lint tidy cluster-up cluster-down integration-test gateway-api-prereqs

BINARY := bin/ktf

build:
	go build -o $(BINARY) ./cmd/ktf

test:
	go test ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

# Local cluster management (requires kind)
cluster-up:
	kind create cluster --name ktf-dev

cluster-down:
	kind delete cluster --name ktf-dev

# Install Gateway API CRDs + cloud-provider-kind (required before gateway-api example)
gateway-api-prereqs:
	bash example/gateway-api/setup.sh

integration-test:
	go test ./... -tags integration

clean:
	rm -rf bin/ coverage.out

coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out
