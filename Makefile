.PHONY: build test run docker-build docker-run clean

build:
	go build -o depot ./cmd/depot

test:
	go test -v ./...

run: build
	./depot

docker-build:
	docker build -t depot:latest .

docker-run:
	docker run -d \
		--name depot \
		-p 8443:8443 \
		-v depot-data:/var/depot/data \
		-v depot-certs:/var/depot/certs \
		depot:latest

clean:
	rm -f depot
	go clean -cache

deps:
	go mod download
	go mod tidy

lint:
	golangci-lint run

cert:
	mkdir -p certs
	openssl genrsa -out certs/server.key 2048
	openssl req -new -x509 -sha256 -key certs/server.key -out certs/server.crt -days 365 \
		-subj "/C=US/ST=State/L=City/O=Organization/CN=localhost"