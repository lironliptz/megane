BINARY := jump-starter
VERSION_FILE := VERSION
VERSION_VAL := $(shell test -f $(VERSION_FILE) && tr -d ' \n\r\t' < $(VERSION_FILE) || echo dev)
LDFLAGS := -X jump-starter/internal/version.LinkVersion=$(VERSION_VAL)

.PHONY: run build test docker clean version fetch-similar collect-consensus

run:
	CGO_ENABLED=1 go run -ldflags "$(LDFLAGS)" ./cmd/server

build:
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/server

# Batch-ingest a curated similar-companies JSON (prompt 13). SIMILAR is
# required; ROOT/YEARS are optional (defaults: ./fileDB, 10 years).
fetch-similar:
	CGO_ENABLED=1 go run ./cmd/fetch-similar \
	  --similar $(SIMILAR) $(if $(ROOT),--root $(ROOT),) $(if $(YEARS),--years $(YEARS),)

collect-consensus:
	CGO_ENABLED=1 go run ./cmd/collect-consensus \
	  --similar $(SIMILAR) $(if $(SNAPSHOT),--snapshot,)

test:
	go test ./...

version:
	@echo $(VERSION_VAL)

docker:
	docker-compose up --build

clean:
	rm -f $(BINARY)
