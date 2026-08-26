export CGO_ENABLED=0

BINARY := akavefs
GOBIN ?= $(shell go env GOBIN)
ifeq ($(strip $(GOBIN)),)
GOBIN := $(shell go env GOPATH)/bin
endif

run-test: s3proxy.jar
	./test/run-tests.sh

run-xfstests: s3proxy.jar xfstests
	./test/run-xfstests.sh

.PHONY: xfstests
xfstests:
	@if [ ! -d xfstests ]; then git clone --depth=1 https://github.com/kdave/xfstests; fi
	@if ! grep -q 'if \[ -z "$$UMOUNT_PROG" \]' xfstests/common/config 2>/dev/null; then \
		cd xfstests && patch -p1 -l < ../test/xfstests.diff; \
	fi

s3proxy.jar:
	@if [ ! -f s3proxy.jar ]; then wget --tries=3 --timeout=60 https://github.com/gaul/s3proxy/releases/download/s3proxy-1.8.0/s3proxy -O s3proxy.jar; fi

get-deps: s3proxy.jar
	go get -t ./...

build:
	go build -o $(BINARY) -ldflags "-X main.Version=`git rev-parse HEAD`"

install: build
	install -Dm755 $(BINARY) $(GOBIN)/$(BINARY)


# Docker build targets
docker-build:
	docker build -f Dockerfile.build -t akavefs-builder:latest .

docker-binary: docker-build
	@echo "Extracting binary from Docker container..."
	@# Create temporary container and copy binary
	@CONTAINER_ID=$$(docker create akavefs-builder:latest) && \
	docker cp $$CONTAINER_ID:/akavefs ./akavefs && \
	docker rm $$CONTAINER_ID && \
	echo "Binary akavefs copied to project root"

docker-clean:
	docker rmi akavefs-builder:latest 2>/dev/null || true

.PHONY: protoc docker-build docker-binary docker-clean
protoc:
	protoc --go_out=. --experimental_allow_proto3_optional --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative core/pb/*.proto
