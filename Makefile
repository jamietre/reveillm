REGISTRY ?= 172.16.2.18:5000
IMAGE     := $(REGISTRY)/reveillm

.PHONY: push
push:
	docker build -t $(IMAGE):latest .
	docker push $(IMAGE):latest
