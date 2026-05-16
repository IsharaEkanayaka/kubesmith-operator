IMAGE ?= kubesmith/operator:latest

.PHONY: build
build:
	go build -o bin/manager ./cmd/main.go

.PHONY: run
run:
	go run ./cmd/main.go

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE) .
