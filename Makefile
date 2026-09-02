.PHONY: test build-linux-arm64

test:
	go test ./...

build-linux-arm64:
	mkdir -p dist
	env CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o dist/my_assistant ./cmd/my_assistant
