CONTROLLER_GEN ?= /Users/hyunsuk/go/bin/controller-gen
IMAGE         ?= lvmpv:latest
KIND_CLUSTER  ?= lvmpv-test

.PHONY: all generate manifests install docker-build kind-load deploy undeploy test-deploy install-tools tidy

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

## Build the driver Docker image
docker-build:
	docker build -t $(IMAGE) .

## Load the image into Kind (no registry needed)
kind-load: docker-build
	kind load docker-image $(IMAGE) --name $(KIND_CLUSTER)

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
