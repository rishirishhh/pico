.PHONY: build run test tidy clean

BINARY := bin/p2pparty

build:
	go build -o $(BINARY) ./cmd/p2pparty

run: build
	./$(BINARY)

test:
	go test ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/
