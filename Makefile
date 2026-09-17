.PHONY: all run build build-desktop build-server build-windows build-all test test-server clean

all: build

run:
	go run cmd/nineguard/main.go

# Auto-detects Desktop Environment / Window Manager vs Server
build:
	@./scripts/build.sh auto

# Explicit desktop build (with system tray, CGO enabled)
build-desktop:
	@./scripts/build.sh desktop

# Explicit server build (headless daemon, pure Go, no tray dependencies)
build-server:
	@./scripts/build.sh server

# Windows build (with system tray)
build-windows:
	@./scripts/build.sh windows

# Build both desktop and server editions
build-all:
	@./scripts/build.sh all

test:
	go test ./...

test-server:
	CGO_ENABLED=0 go test -tags server ./...

clean:
	rm -f nineguard nineguard-server nineguard.exe

