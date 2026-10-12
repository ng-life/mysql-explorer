GO ?= go
COMMAND := ./cmd/mysql-explorer

.PHONY: build run test vet fmt check web
web:
	pnpm install --frozen-lockfile
	pnpm build
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
