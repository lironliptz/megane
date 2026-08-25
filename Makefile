BINARY := jump-starter
VERSION_FILE := VERSION
VERSION_VAL := $(shell test -f $(VERSION_FILE) && tr -d ' \n\r\t' < $(VERSION_FILE) || echo dev)
LDFLAGS := -X jump-starter/internal/version.LinkVersion=$(VERSION_VAL)

.PHONY: run build test docker clean version

run:
	CGO_ENABLED=1 go run -ldflags "$(LDFLAGS)" ./cmd/server

build:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/server

test:
	go test ./...

version:
	@echo $(VERSION_VAL)

docker:
	docker-compose up --build

clean:
	rm -f $(BINARY)
