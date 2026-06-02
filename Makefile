BIN := $(HOME)/.local/bin

.PHONY: build test install clean

build:
	go build ./cmd/smc-mixerd
	go build ./cmd/smc-mixer

test:
	go test ./...

install: build
	install -Dm755 smc-mixerd $(BIN)/smc-mixerd
	install -Dm755 smc-mixer  $(BIN)/smc-mixer

clean:
	rm -f smc-mixerd smc-mixer
