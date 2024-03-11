TEST?=./...
OPENSEARCH_URLS ?= https://127.0.0.1:9200
OPENSEARCH_USERNAME ?= admin
OPENSEARCH_PASSWORD ?= vLPeJYa8.3RqtZCcAK6jNz

all: help

build: fmt
	go build .

loadFixture:
	curl --fail -u admin:vLPeJYa8.3RqtZCcAK6jNz -k -H "Content-Type: application/x-ndjson" -XPOST https://localhost:9200/logs/_bulk?pretty --data-binary @fixtures/logs/bulk.ndjson

test: fmt
	OPENSEARCH_URLS=${OPENSEARCH_URLS} OPENSEARCH_USERNAME=${OPENSEARCH_USERNAME} OPENSEARCH_PASSWORD=${OPENSEARCH_PASSWORD} go test $(TEST) -v -count 1 -parallel 1 -race -coverprofile=cover.out -covermode=atomic $(TESTARGS) -timeout 120m

fmt:
	@echo "==> Fixing source code with gofmt..."
	gofmt -s -w ./
