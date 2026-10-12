GO ?= go
COMMAND := ./cmd/mysql-explorer

.PHONY: build run test vet fmt check
build:
	$(GO) build -trimpath -o dist/mysql-explorer $(COMMAND)
run:
	$(GO) run $(COMMAND) $(ARGS)
test:
	$(GO) test -race ./...
vet:
	$(GO) vet ./...
fmt:
	$(GO) fmt ./...
check: test vet
