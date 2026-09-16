.PHONY: run build build-windows test

run:
	go run cmd/nineguard/main.go

build:
	go build -o nineguard cmd/nineguard/main.go

build-windows:
	GOOS=windows GOARCH=amd64 go build -o nineguard.exe cmd/nineguard/main.go

test:
	go test ./...
