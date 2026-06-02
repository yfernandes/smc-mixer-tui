BIN := $(HOME)/.local/bin

.PHONY: build test test-pipewire-integration install clean

build:
	go build ./cmd/smc-mixerd
	go build ./cmd/smc-mixer

test:
	go test ./...

test-pipewire-integration:
	go test -tags integration_pipewire ./pipewire -run TestCrossfaderBuildsSendBusGraphAndLeavesMastersIndependent -count=1 -v

install: build
	install -Dm755 smc-mixerd $(BIN)/smc-mixerd
	install -Dm755 smc-mixer  $(BIN)/smc-mixer

clean:
	rm -f smc-mixerd smc-mixer
