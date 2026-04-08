PUSH_REGISTRY ?= your-registry
PULL_REGISTRY ?= your-registry
IMAGE         := $(PUSH_REGISTRY)/whisper-bot
NAMESPACE     := whisper-watch
CHART         := ./chart

TAG := $(shell git rev-parse --short HEAD)

LOCAL_VALUES := chart/values.local.yaml
HELM_LOCAL   := $(if $(wildcard $(LOCAL_VALUES)),-f $(LOCAL_VALUES),)

.PHONY: build deploy ship test

build:
	docker buildx build \
		--builder multiarch \
		--platform linux/amd64 \
		--output "type=image,name=$(IMAGE):$(TAG),push=true,registry.insecure=true" \
		.

deploy:
	helm upgrade $(NAMESPACE) $(CHART) \
		-n $(NAMESPACE) \
		$(HELM_LOCAL) \
		--set whisperBot.imageTag=$(TAG) \
		--set whisperBot.image=$(PULL_REGISTRY)/whisper-bot

ship: build deploy

test:
	docker run --rm \
		-v $(PWD):/app -w /app \
		golang:1.23-alpine \
		go test ./...
