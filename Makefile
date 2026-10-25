VERSION ?= $(shell cat VERSION)-dev
NAME    := confluence-exporter

.PHONY: build test dist clean

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(NAME) .

test:
	go vet ./...
	go test ./...

# Single-file binaries for every platform, ready to hand out.
dist:
	scripts/build.sh $(VERSION)

clean:
	rm -rf bin dist
