# Image registry and tag used by all build/push targets
REGISTRY ?= registry.kdenv.lab
TAG      ?= dev

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool used for building images.
# Replace with podman or another tool if needed.
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Add Hermit bin directory to PATH for all make targets
export PATH := $(shell pwd)/bin:$(PATH)

# Kubernetes / envtest versions
ENVTEST_K8S_VERSION ?= 1.33

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL        ?= kubectl
KIND           ?= kind
KUSTOMIZE      ?= kustomize
CONTROLLER_GEN ?= controller-gen
ENVTEST        ?= setup-envtest
GOLANGCI_LINT   = golangci-lint
HELM           ?= helm
HELM_DOCS      ?= helm-docs

## Tool Binaries (Testing)
GINKGO ?= $(LOCALBIN)/ginkgo

## Image name
IMAGE = $(REGISTRY)/kubernetes-landscape-orchestrator:$(TAG)

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: rbac
rbac: hermit ## generate ClusterRole and include it in Helm chart
	$(CONTROLLER_GEN) rbac:roleName=landscape-orchestrator-manager crd webhook \
		paths="./internal/..." \
		output:rbac:artifacts:config=config/rbac
		charts/patch-clusterrole.sh kubernetes-landscape-orchestrator "config/rbac/role.yaml" "charts/kubernetes-landscape-orchestrator/templates/clusterrole.yaml"

.PHONY: fmt
fmt: hermit ## Run go fmt against the entire codebase.
	go fmt ./...

.PHONY: vet
vet: hermit ## Run go vet against the entire codebase.
	go vet ./...

.PHONY: lint
lint: hermit ## Run golangci-lint against the entire codebase.
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: hermit ## Run golangci-lint and apply automatic fixes.
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config: hermit ## Verify the golangci-lint configuration.
	$(GOLANGCI_LINT) config verify

##@ Testing

.PHONY: test
test: hermit generate-test-crds fmt vet setup-envtest ginkgo ## Run all unit tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
		$(GINKGO) --coverprofile=cover.out -v \
		./internal/fluxdeployer/... \
		./internal/taskexecution/... \
		./internal/activationexecution/... \
		./cmd/vectordata/...

.PHONY: generate-test-crds
generate-test-crds: hermit ## Generate CRDs needed for controller tests.
	$(CONTROLLER_GEN) crd paths="github.com/konfidence-project/konfidence/api/v1alpha1/..." output:crd:artifacts:config=test/data/crds/konfidence
	@# keep only the CRDs relevant to this orchestrator
	@rm -f \
		test/data/crds/konfidence/konfidence.cloud_projects.yaml \
		test/data/crds/konfidence/konfidence.cloud_stageconfigurations.yaml \
		test/data/crds/konfidence/konfidence.cloud_vectorpromotions.yaml \
		test/data/crds/konfidence/konfidence.cloud_vectorpromotionconfigs.yaml \
		test/data/crds/konfidence/konfidence.cloud_vectortemplates.yaml

.PHONY: setup-envtest
setup-envtest: hermit ## Download the envtest binaries for the configured Kubernetes version.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@$(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: ginkgo
ginkgo: hermit ## Install ginkgo CLI to LOCALBIN.
	go build -o $(LOCALBIN)/ginkgo github.com/onsi/ginkgo/v2/ginkgo

##@ Build

.PHONY: build
build: hermit fmt vet ## Build the operator binary.
	go build -o bin/kubernetes-landscape-orchestrator main.go

.PHONY: build-vector-data-service
build-vector-data-service: hermit fmt vet ## Build the vector-data service binary.
	go build -o bin/vector-data-service ./cmd/vectordata

.PHONY: run
run: hermit fmt vet ## Run the operator from your host.
	go run main.go

# This target is only used for local environments (not in pipeline)
.PHONY: docker-build
docker-build: hermit ## Build the container image (local use only).
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/kubernetes-landscape-orchestrator main.go
	$(CONTAINER_TOOL) build -t $(IMAGE) .

.PHONY: docker-bake
docker-bake: hermit ## Build the container image with docker buildx bake.
	$(CONTAINER_TOOL) buildx bake --file docker-bake.hcl

.PHONY: docker-push
docker-push: ## Push the container image.
	$(CONTAINER_TOOL) push $(IMAGE)

##@ Developer Setup

.PHONY: hermit
hermit: ## Check that Hermit is installed and its environment is activated.
	@command -v hermit >/dev/null 2>&1 || { \
		echo "Hermit is not installed. Please install it from https://cashapp.github.io/hermit/"; \
		exit 1; \
	}
	@hermit status >/dev/null 2>&1 || { \
		echo "Hermit environment is not activated. Run 'source ./bin/activate-hermit' or 'eval \"\$$(hermit env)\"'"; \
		exit 1; \
	}

.PHONY: install-git-hooks
install-git-hooks: hermit ## Install git hooks via prek.
	@echo "Setting up prek (pre-commit) installing git hooks..."
	prek install

.PHONY: uninstall-git-hooks
uninstall-git-hooks: hermit ## Uninstall git hooks via prek.
	@echo "Uninstalling prek (pre-commit) git hooks..."
	prek uninstall

##@ Helm

.PHONY: helm-install
helm-install: hermit ## Install the landscape-orchestrator chart into the current cluster.
	$(HELM) upgrade --install kubernetes-landscape-orchestrator charts/kubernetes-landscape-orchestrator

.PHONY: helm-install-vector-data-service
helm-install-vector-data-service: hermit ## Install the vector-data-service chart into the current cluster.
	$(HELM) upgrade --install vector-data-service charts/vector-data-service

.PHONY: helm-uninstall
helm-uninstall: hermit ## Uninstall the landscape-orchestrator helm release.
	$(HELM) uninstall kubernetes-landscape-orchestrator --ignore-not-found

.PHONY: helm-uninstall-vector-data-service
helm-uninstall-vector-data-service: hermit ## Uninstall the vector-data-service helm release.
	$(HELM) uninstall vector-data-service --ignore-not-found

.PHONY: helm-docs
helm-docs: hermit ## Generate Helm chart documentation (charts/*/README.md).
	$(HELM_DOCS) --chart-search-root charts
