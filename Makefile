BINARY  := terraform-provider-buildkit
VERSION ?= dev

default: build

.PHONY: build
build:
	go build -o $(BINARY) -ldflags "-X main.version=$(VERSION)" .

.PHONY: install
install:
	go install -ldflags "-X main.version=$(VERSION)" .

.PHONY: fmt
fmt:
	gofmt -s -w .

.PHONY: vet
vet:
	go vet ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: tidy
tidy:
	go mod tidy

# unit tests only.
.PHONY: test
test:
	go test -v -cover ./...

# acceptance tests; require a live buildkit endpoint (set BUILDKIT_HOST or rely
# on auto-discovery) and TF_ACC=1.
.PHONY: testacc
testacc:
	TF_ACC=1 go test -v -cover -timeout 30m ./...

# generate provider docs into docs/ using terraform-plugin-docs.
.PHONY: docs
docs:
	go generate ./...

.PHONY: clean
clean:
	rm -f $(BINARY)
