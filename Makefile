.PHONY: build build-linux run test lint clean docker-build docker-run

build:
	go build -o bin/proxy ./cmd/proxy/

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/proxy ./cmd/proxy/

run:
	go run ./cmd/proxy/

test:
	go test -race -cover ./...

test-integration:
	go test -v ./tests/integration/...

lint:
	go vet ./...

clean:
	rm -rf bin/

docker-build: build-linux
	docker build -t major1201/anthropic-proxy-go .

docker-run:
	docker run -p 8000:8000 -v ./config:/app/config major1201/anthropic-proxy-go

container-build: build-linux
	container build -a amd64 -t major1201/anthropic-proxy-go .
