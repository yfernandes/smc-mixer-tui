BIN := $(HOME)/.local/bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build test test-pipewire-integration install reinstall clean

build:
	go build $(LDFLAGS) ./cmd/smc-mixerd
	go build $(LDFLAGS) ./cmd/smc-mixer

test:
	go test ./...

test-pipewire-integration:
	go test -tags integration_pipewire ./pipewire -run TestCrossfaderBuildsSendBusGraphAndLeavesMastersIndependent -count=1 -v

install: build
	install -Dm755 smc-mixerd $(BIN)/smc-mixerd
	install -Dm755 smc-mixer  $(BIN)/smc-mixer

reinstall: install
	@echo "Stopping smc-mixerd ($(VERSION))…"
	@pkill -SIGTERM smc-mixerd 2>/dev/null || true
	@i=0; while pgrep -x smc-mixerd > /dev/null 2>&1 && [ $$i -lt 20 ]; do sleep 0.25; i=$$((i+1)); done
	@echo "Starting smc-mixerd…"
	@$(BIN)/smc-mixerd &
	@echo "Done — daemon started as version $(VERSION)."

clean:
	rm -f smc-mixerd smc-mixer
