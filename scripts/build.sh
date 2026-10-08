#!/usr/bin/env bash
set -euo pipefail

# NineGuard Build Script
# Automatically differentiates Desktop (with System Tray) vs Server (Headless Daemon, pure Go)

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

GIT_VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo "v1.1.0")}"
GIT_COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo "dev")}"
LDFLAGS="-s -w -X 'nineguard/internal/version.Version=${GIT_VERSION}' -X 'nineguard/internal/version.Commit=${GIT_COMMIT}'"

detect_environment() {
    local os="$(uname -s)"
    case "$os" in
        MINGW*|MSYS*|CYGWIN*|Windows_NT)
            echo "desktop"
            return
            ;;
        Darwin)
            echo "desktop"
            return
            ;;
    esac

    # Linux detection:
    # 1. Check for graphical display session
    if [ -n "${DISPLAY:-}" ] || [ -n "${WAYLAND_DISPLAY:-}" ]; then
        echo "desktop"
        return
    fi

    # 2. Check for desktop environment or window manager variables
    if [ -n "${XDG_CURRENT_DESKTOP:-}" ] || [ -n "${DESKTOP_SESSION:-}" ] || [ -n "${GDMSESSION:-}" ] || [ -n "${WINDOWMANAGER:-}" ]; then
        echo "desktop"
        return
    fi

    # 3. Check for running graphical desktop or window manager processes
    if pgrep -x "Xorg" >/dev/null 2>&1 || pgrep -x "Xwayland" >/dev/null 2>&1 || \
       pgrep -f "gnome-shell|kwin|plasma|sway|i3|xfwm4|openbox|wayfire|hyprland|fluxbox" >/dev/null 2>&1; then
        echo "desktop"
        return
    fi

    # 4. Fallback: Linux server / headless environment
    echo "server"
}

build_desktop() {
    local outfile="${1:-nineguard}"
    echo "📦 Building NineGuard for Desktop (with System Tray)..."
    echo "   Target:     $outfile"
    echo "   Version:    $GIT_VERSION ($GIT_COMMIT)"
    echo "   CGO:        Enabled (systray support)"
    echo "   Platform:   $(go env GOOS)/$(go env GOARCH)"
    CGO_ENABLED=1 go build -ldflags="$LDFLAGS" -o "$outfile" cmd/nineguard/main.go
    local size=$(du -h "$outfile" | cut -f1)
    echo "✓ Built Desktop binary: $outfile ($size)"
}

build_server() {
    local outfile="${1:-nineguard}"
    echo "📦 Building NineGuard for Server (Headless Daemon, no Tray)..."
    echo "   Target:     $outfile"
    echo "   Version:    $GIT_VERSION ($GIT_COMMIT)"
    echo "   CGO:        Disabled (pure Go, zero C library dependencies)"
    echo "   Tags:       server"
    echo "   Platform:   $(go env GOOS)/$(go env GOARCH)"
    CGO_ENABLED=0 go build -tags server -ldflags="$LDFLAGS" -o "$outfile" cmd/nineguard/main.go
    local size=$(du -h "$outfile" | cut -f1)
    echo "✓ Built Server binary: $outfile ($size)"
}

build_windows() {
    local outfile="${1:-nineguard.exe}"
    echo "📦 Building NineGuard for Windows..."
    echo "   Target:     $outfile"
    echo "   Version:    $GIT_VERSION ($GIT_COMMIT)"
    echo "   Platform:   windows/amd64"
    GOOS=windows GOARCH=amd64 go build -ldflags="$LDFLAGS" -o "$outfile" cmd/nineguard/main.go
    local size=$(du -h "$outfile" | cut -f1)
    echo "✓ Built Windows binary: $outfile ($size)"
}

build_docker() {
    local tag="${1:-$GIT_VERSION}"
    echo "📦 Building NineGuard Docker Image..."
    echo "   Version:    $GIT_VERSION"
    echo "   Commit:     $GIT_COMMIT"
    echo "   Tag:        $tag"
    docker build \
        --build-arg VERSION="$GIT_VERSION" \
        --build-arg COMMIT="$GIT_COMMIT" \
        -t "nineguard:$tag" \
        -t "nineguard:latest" .
    echo "✓ Built Docker image: nineguard:$tag and nineguard:latest"
}

build_all() {
    echo "📦 Building all NineGuard editions..."
    echo ""
    build_desktop "nineguard"
    echo ""
    build_server "nineguard-server"
    echo ""
    build_windows "nineguard.exe"
    echo ""
    echo "✓ All builds finished:"
    ls -lh nineguard nineguard-server nineguard.exe
}

build_auto() {
    local env="$(detect_environment)"
    echo "🔍 Environment detected: $env"
    if [ "$env" = "desktop" ]; then
        echo "💡 Desktop Environment / Window Manager detected. Building with System Tray."
        build_desktop "nineguard"
    else
        echo "💡 Server / Headless environment detected. Building Server edition (pure Go, daemon)."
        build_server "nineguard"
    fi
}

MODE="${1:-auto}"

case "$MODE" in
    auto)
        build_auto
        ;;
    desktop)
        build_desktop "nineguard"
        ;;
    server)
        build_server "nineguard"
        ;;
    windows)
        build_windows "nineguard.exe"
        ;;
    docker)
        build_docker "${2:-$GIT_VERSION}"
        ;;
    all)
        build_all
        ;;
    help|--help|-h)
        echo "NineGuard Build Tool"
        echo "Usage: $0 [auto|desktop|server|windows|all]"
        echo ""
        echo "Modes:"
        echo "  auto     (default) Detects Desktop Environment / Window Manager vs Server"
        echo "  desktop  Builds desktop edition with system tray (CGO_ENABLED=1)"
        echo "  server   Builds server edition without tray (pure Go, CGO_ENABLED=0, -tags server)"
        echo "  windows  Cross-compiles for Windows (amd64)"
        echo "  docker   Builds Docker image with injected version & commit tags"
        echo "  all      Builds desktop, server, and windows editions"
        ;;
    *)
        echo "Unknown mode: $MODE"
        echo "Run '$0 --help' for usage."
        exit 1
        ;;
esac
