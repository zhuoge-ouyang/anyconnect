.PHONY: build run clean test

build:
	go build -ldflags="-H windowsgui" -o bin/split-tunnel.exe ./cmd/

run:
	go run ./cmd/

clean:
	rm -rf bin/

test:
	go test ./internal/... -v
