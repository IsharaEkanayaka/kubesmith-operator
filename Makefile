IMAGE_TAG   ?= latest
IMAGE_NAME  ?= docker.io/kubesmith/kubesmith-operator
IMG         := $(IMAGE_NAME):$(IMAGE_TAG)

# Tooling
CONTROLLER_GEN ?= $(shell which controller-gen 2>/dev/null || echo "go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0")

.PHONY: all build test generate manifests install uninstall deploy docker-build docker-push

all: build

## ── Code generation ──────────────────────────────────────────────────────────

# Re-generate DeepCopy methods and CRD manifests from Go type annotations.
generate:
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

manifests:
	$(CONTROLLER_GEN) crd rbac:roleName=kubesmith-operator-manager-role \
	  paths="./..." output:crd:artifacts:config=config/crd/bases

## ── Build ────────────────────────────────────────────────────────────────────

build:
	go build ./...

test:
	go test ./... -coverprofile cover.out

## ── Cluster install / uninstall ─────────────────────────────────────────────

# Apply CRDs to the current cluster (requires cluster access).
install:
	kubectl apply -f config/crd/bases/

# Remove CRDs from the current cluster.
uninstall:
	kubectl delete -f config/crd/bases/ --ignore-not-found

# Deploy the operator controller manager.
deploy:
	kubectl apply -f config/rbac/service_account.yaml
	kubectl apply -f config/rbac/role.yaml
	kubectl apply -f config/rbac/role_binding.yaml
	kubectl apply -f config/manager/manager.yaml

undeploy:
	kubectl delete -f config/manager/manager.yaml --ignore-not-found
	kubectl delete -f config/rbac/role_binding.yaml --ignore-not-found
	kubectl delete -f config/rbac/role.yaml --ignore-not-found
	kubectl delete -f config/rbac/service_account.yaml --ignore-not-found

## ── Docker ───────────────────────────────────────────────────────────────────

docker-build:
	docker build -t $(IMG) .

docker-push:
	docker push $(IMG)

docker-build-push: docker-build docker-push

## ── Convenience ──────────────────────────────────────────────────────────────

# Full local dev cycle: build image and load into kind cluster.
kind-load: docker-build
	kind load docker-image $(IMG)
