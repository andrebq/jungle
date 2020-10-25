.PHONY: test fmt watch

test:
	go test ./...

watch:
	modd

fmt:
	go mod tidy
	go fmt ./...
