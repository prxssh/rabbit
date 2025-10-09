BINARY := rabbit
BUILD_DIR := build

.PHONY: clean format test

docker-build:
	mkdir -p ${BUILD_DIR}
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ${BUILD_DIR}/${BINARY} .

run:
	wails dev

clean: 
	go clean 
	rm -rf build

format: 
	golines -m 100 -t 8 --shorten-comments -w .
	gofmt -w .

test: 
	go test ./...
