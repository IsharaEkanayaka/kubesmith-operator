IMG ?= kubesmith-operator:latest

.PHONY: build run test docker-build docker-push install

build:
	go build -o bin/manager ./cmd/main.go

run:
	go run ./cmd/main.go

test:
	go test ./... -v -coverprofile=coverage.out

test-coverage: test
	go tool cover -html=coverage.out

install:
	kubectl apply -f config/crd/bases/platform.kubesmith.io_applications.yaml

docker-build:
	docker build -t $(IMG) .

docker-push:
	docker push $(IMG)

fmt:
	go fmt ./...

vet:
	go vet ./...
