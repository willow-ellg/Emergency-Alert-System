BIN := bin
HUB_ADMIN_TOKEN ?= dev-token
DATA ?= ./data/state.json
SNAPSHOT ?= ./snapshot
HUB_ADDR ?= :8080
BEACON_ADDR ?= :8081
DEMO ?= true

.PHONY: all build test vet fmt run hub beacon snapshot seed clean

all: build

build:
	mkdir -p $(BIN)
	go build -trimpath -ldflags "-s -w" -o $(BIN)/hub ./cmd/hub
	go build -trimpath -ldflags "-s -w" -o $(BIN)/beacon ./cmd/beacon

test:
	go test ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

run: build
	HUB_ADMIN_TOKEN=$(HUB_ADMIN_TOKEN) $(BIN)/hub -addr $(HUB_ADDR) -data $(DATA) -snapshot-dir $(SNAPSHOT) -demo=$(DEMO)

hub: run

beacon: build
	$(BIN)/beacon serve -dir $(SNAPSHOT) -addr $(BEACON_ADDR)

snapshot: build
	$(BIN)/beacon render -dir $(SNAPSHOT) -data $(DATA) -demo=$(DEMO)

seed: build
	$(BIN)/hub -seed-only -data $(DATA)

clean:
	rm -rf $(BIN) data snapshot
