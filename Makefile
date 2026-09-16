.PHONY: run build test

run:
	go run cmd/nineguard/main.go

build:
	go build -o nineguard cmd/nineguard/main.go

test:
	go test ./...
