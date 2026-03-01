CONTROLLER_GEN    ?= /Users/hyunsuk/go/bin/controller-gen
DOCKER_USER       ?= banghsk99
TAG               ?= latest
CONTROLLER_IMAGE  ?= $(DOCKER_USER)/lvmpv-controller:$(TAG)
NODE_IMAGE        ?= $(DOCKER_USER)/lvmpv-node:$(TAG)
KIND_CLUSTER      ?= lvmpv-test

.PHONY: all generate manifests install docker-build docker-push kind-load deploy undeploy test-deploy install-tools tidy

all: generate manifests

## Generate DeepCopy methods (zz_generated.deepcopy.go)
generate:
	$(CONTROLLER_GEN) object paths="./api/..."

## Generate CRD YAML manifests into deploy/crds/
manifests:
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:dir=deploy/crds

## Apply CRDs to the current cluster
install: manifests
	kubectl apply -f deploy/crds/

## Download controller-gen
install-tools:
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

## Build both Docker images
docker-build:
	docker build -f Dockerfile.controller -t $(CONTROLLER_IMAGE) .
	docker build -f Dockerfile.node       -t $(NODE_IMAGE) .

## Push both images to Docker Hub
docker-push: docker-build
	docker push $(CONTROLLER_IMAGE)
	docker push $(NODE_IMAGE)

## Load both images into Kind (no registry needed)
kind-load: docker-build
	kind load docker-image $(CONTROLLER_IMAGE) --name $(KIND_CLUSTER)
	kind load docker-image $(NODE_IMAGE)        --name $(KIND_CLUSTER)

## Deploy everything to the current cluster
deploy: manifests kind-load
	kubectl apply -f deploy/crds/
	kubectl apply -f deploy/csidriver.yaml
	kubectl apply -f deploy/rbac.yaml
	kubectl apply -f deploy/controller.yaml
	kubectl apply -f deploy/node-ds.yaml
	kubectl apply -f deploy/storageclass.yaml

## Remove all driver resources from the cluster
undeploy:
	kubectl delete -f deploy/storageclass.yaml --ignore-not-found
	kubectl delete -f deploy/node-ds.yaml --ignore-not-found
	kubectl delete -f deploy/controller.yaml --ignore-not-found
	kubectl delete -f deploy/rbac.yaml --ignore-not-found
	kubectl delete -f deploy/csidriver.yaml --ignore-not-found
	kubectl delete -f deploy/crds/ --ignore-not-found

## Apply the test PVC + Pod
test-deploy:
	kubectl apply -f deploy/test/pvc-pod.yaml

## Tidy go modules
tidy:
	go mod tidy
