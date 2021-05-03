# Quick Start
# -----------
# make production:
#     Used by CI builds and what is released to customers.
#
# make:
#     builds your changes and updates planet binary in
#     build/current/rootfs/usr/bin/planet
#
# make start:
#     starts Planet from build/dev/rootfs/usr/bin/planet
#
# Build Steps
# -----------
# The sequence of steps the build process takes:
#     1. Make 'os' Docker image: the empty Debian 8 image.
#     2. Make 'base' image on top of 'os' (Debian + our additions)
#     3. Make 'buildbox' image on top of 'os'. Used for building,
#        not part of the Planet image.
#     4. Build various components (flannel, etcd, k8s, etc) inside
#        of the 'buildbox' based on inputs (master/node/dev)
#     5. Store everything inside a temporary Docker image based on 'base'
#     6. Export the root FS of that image into build/current/rootfs
#     7. build/current/rootfs is basically the output of the build.
#     8. Last, rootfs is stored into a ready for distribution tarball.
#
.DEFAULT_GOAL := all

MKFILE_PATH := $(abspath $(lastword $(MAKEFILE_LIST)))
CURRENT_DIR := $(realpath $(patsubst %/,%,$(dir $(MKFILE_PATH))))

ASSETS := $(CURRENT_DIR)/build.assets
BUILD_ASSETS := $(CURRENT_DIR)/build/assets
BUILDDIR ?= $(CURRENT_DIR)/build
OUTPUTDIR := $(BUILDDIR)/planet

KUBE_VER ?= v1.21.0
SECCOMP_VER ?= 2.3.1-2.1+deb9u1
DOCKER_VER ?= 19.03.12
# we currently use our own flannel fork: gravitational/flannel
FLANNEL_VER := v0.10.5-gravitational
HELM_VER := 2.16.12
HELM3_VER := 3.3.4
COREDNS_VER := 1.7.0
NODE_PROBLEM_DETECTOR_VER := v0.6.4
CNI_VER := 0.8.6
SERF_VER := v0.8.5
IPTABLES_VER := v1.8.5

# planet user to use inside the rootfs tarball. This serves as a placeholder
# and the files will be owned by the actual planet user after extraction
PLANET_UID ?= 980665
PLANET_GID ?= 980665

# ETCD Versions to include in the release
# This list needs to include every version of etcd that we can upgrade from + latest
# Version log
# v3.3.4
# v3.3.9  - 5.2.x,
# v3.3.11 - 5.5.x,
# v3.3.12 - 6.3.x, 6.1.x, 5.5.x
# v3.3.15 - 6.3.x
# v3.3.20 - 6.3.x, 6.1.x, 5.5.x
# v3.3.22 - 6.3.x, 6.1.x, 5.5.x
# v3.4.3  - 7.0.x
# v3.4.7  - 7.0.x
# v3.4.9   - 7.0.x
ETCD_VER := v3.3.12 v3.3.15 v3.3.20 v3.3.22 v3.4.3 v3.4.7 v3.4.9
# This is the version of etcd we should upgrade to (from the version list)
# Note: When bumping the ETCD_LATEST_VERSION, please ensure that:
#   - The version of etcd vendored as a library is the same (Gopkg.toml)
#   - Modify build.go and run the etcd upgrade integration test (go run mage.go ci:testEtcdUpgrade)
ETCD_LATEST_VER := v3.4.9

# TODO: currently defaulting to stretch explicitly to work around
# a breaking change in buster (with GLIBC 2.28) w.r.t fcntl() implementation.
# See https://forum.rebol.info/t/dynamic-linking-static-linking-and-the-march-of-progress/1231/1
BUILDBOX_GO_VER ?= go1.13.8-stretch
PLANET_BUILD_TAG ?= $(shell git describe --tags)
PLANET_IMAGE_NAME ?= planet/base
PLANET_IMAGE ?= $(PLANET_IMAGE_NAME):$(PLANET_BUILD_TAG)
PLANET_OS_NAME ?= planet/os
PLANET_OS_IMAGE ?= $(PLANET_OS_NAME):$(PLANET_BUILD_TAG)
PLANET_BUILDBOX_NAME ?=planet/buildbox
PLANET_BUILDBOX_IMAGE ?= $(PLANET_BUILDBOX_NAME):$(PLANET_BUILD_TAG)
export

PUBLIC_IP ?= 127.0.0.1
PLANET_PACKAGE_PATH := $(CURRENT_DIR)
PLANET_PACKAGE := github.com/gravitational/planet
PLANET_VERSION_PACKAGE_PATH := $(PLANET_PACKAGE)/Godeps/_workspace/src/github.com/gravitational/version
GO_FILES := $(shell find . -type f -name '*.go' -not -path "./vendor/*" -not -path "./build/*")
# Space separated patterns of packages to skip
IGNORED_PACKAGES := /vendor build/

.PHONY: all
all: production image

# 'make build' compiles the Go portion of Planet, meant for quick & iterative
# development on an _already built image_. You need to build an image first, for
# example with "make dev"
.PHONY: build
build: $(BUILD_ASSETS)/planet $(BUILDDIR)/planet.tar.gz
	cp -f $< $(OUTPUTDIR)/rootfs/usr/bin/

.PHONY: planet-bin
planet-bin:
	GO111MODULE=on go build -mod=vendor -o $(OUTPUTDIR)/planet github.com/gravitational/planet/tool/planet

# Deploys the build artifacts to Amazon S3
.PHONY: dev-deploy
dev-deploy:
	$(MAKE) -C $(ASSETS)/makefiles/deploy deploy-s3

# Deploys the build artifacts to Amazon S3 and quay.io
.PHONY: deploy
deploy:
	$(MAKE) -C $(ASSETS)/makefiles/deploy deploy

.PHONY: production
production: $(BUILDDIR)/planet.tar.gz

.PHONY: image
image:
	$(MAKE) -C $(ASSETS)/makefiles/deploy image

$(BUILD_ASSETS)/planet:
	GOOS=linux GOARCH=amd64 GO111MODULE=on \
	     go install -mod=vendor -ldflags "$(PLANET_GO_LDFLAGS)" \
	     $(PLANET_PACKAGE)/tool/planet -o $@

$(BUILDDIR)/planet.tar.gz: buildbox Makefile $(wildcard build.assets/**/*) $(GO_FILES)
	$(MAKE) -C $(ASSETS)/makefiles -e \
		PLANET_BASE_IMAGE=$(PLANET_IMAGE) \
		TARGETDIR=$(OUTPUTDIR) \
		-f buildbox.mk

.PHONY: enter-buildbox
enter-buildbox:
	$(MAKE) -C $(ASSETS)/makefiles -e -f buildbox.mk enter-buildbox

# Run package tests
.PHONY: test
test: remove-temp-files
	go test -race -v -test.parallel=1 ./tool/... ./lib/...

.PHONY: test-package-with-etcd
test-package-with-etcd: remove-temp-files
	PLANET_TEST_ETCD_NODES=http://127.0.0.1:4001 go test -v -test.parallel=0 ./$(p)

.PHONY: remove-temp-files
remove-temp-files:
	find . -name flymake_* -delete

# Start the planet container locally
.PHONY: start
start: build prepare-to-run
	cd $(OUTPUTDIR) && sudo rootfs/usr/bin/planet start \
		--debug \
		--etcd-member-name=local-planet \
		--secrets-dir=/var/planet/state \
		--public-ip=$(PUBLIC_IP) \
		--role=master \
		--service-uid=1000 \
		--initial-cluster=local-planet:$(PUBLIC_IP) \
		--volume=/var/planet/agent:/ext/agent \
		--volume=/var/planet/etcd:/ext/etcd \
		--volume=/var/planet/registry:/ext/registry \
		--volume=/var/planet/docker:/ext/docker

# Stop the running planet container
.PHONY: stop
stop:
	cd $(OUTPUTDIR) && sudo rootfs/usr/bin/planet --debug stop

# Enter the running planet container
.PHONY: enter
enter:
	cd $(OUTPUTDIR) && sudo rootfs/usr/bin/planet enter --debug /bin/bash

# Build the base Docker image everything else is based on.
.PHONY: os
os:
	@echo -e "\n---> Making Planet/OS (Debian) Docker image...\n"
	$(MAKE) -e BUILDIMAGE=$(PLANET_OS_IMAGE) DOCKERFILE=os.dockerfile make-docker-image

# Build the image with components required for running a Kubernetes node
.PHONY: base
base: os
	@echo -e "\n---> Making Planet/Base Docker image based on Planet/OS...\n"
	$(MAKE) -e BUILDIMAGE=$(PLANET_IMAGE) DOCKERFILE=base.dockerfile \
		EXTRA_ARGS="--build-arg SECCOMP_VER=$(SECCOMP_VER) --build-arg IPTABLES_VER=$(IPTABLES_VER) --build-arg PLANET_UID=$(PLANET_UID) --build-arg PLANET_GID=$(PLANET_GID) --build-arg PLANET_OS_IMAGE=$(PLANET_OS_IMAGE)" \
		make-docker-image

# Build a container used for building the planet image
.PHONY: buildbox
buildbox: base
	@echo -e "\n---> Making Planet/BuildBox Docker image:\n" ;\
	$(MAKE) -e BUILDIMAGE=$(PLANET_BUILDBOX_IMAGE) \
		DOCKERFILE=buildbox.dockerfile \
		EXTRA_ARGS="--build-arg GOVERSION=$(BUILDBOX_GO_VER) --build-arg PLANET_BASE_IMAGE=$(PLANET_IMAGE)" \
		make-docker-image

# Remove build artifacts
.PHONY: clean
clean:
	$(MAKE) -C $(ASSETS)/makefiles -f buildbox.mk clean
	rm -rf $(BUILDDIR)

.PHONY: dev-clean
dev-clean:
	$(MAKE) -C $(ASSETS)/makefiles -f buildbox.mk clean
	rm -rf $(BUILDDIR)/planet

# internal use:
.PHONY: make-docker-image
make-docker-image:
	cd $(ASSETS)/docker; docker build $(EXTRA_ARGS) -t $(BUILDIMAGE) -f $(DOCKERFILE) .

.PHONY: remove-godeps
remove-godeps:
	rm -rf Godeps/
	find . -iregex .*go | xargs sed -i 's:".*Godeps/_workspace/src/:":g'

.PHONY: prepare-to-run
prepare-to-run: build
	@sudo mkdir -p /var/planet/registry /var/planet/etcd /var/planet/docker
	@sudo chown $$USER:$$USER /var/planet/etcd -R
	@cp -f $(BUILD_ASSETS)/planet $(OUTPUTDIR)/rootfs/usr/bin/planet

.PHONY: clean-containers
clean-containers:
	@echo -e "\n---> Removing dead Docker/planet containers...\n"
	DEADCONTAINTERS=$$(docker ps --all | grep "planet" | awk '{print $$1}') ;\
	if [ ! -z "$$DEADCONTAINTERS" ] ; then \
		docker rm -f $$DEADCONTAINTERS ;\
	fi

.PHONY: clean-images
clean-images: clean-containers
	@echo -e "\n---> Removing old Docker/planet images...\n"
	DEADIMAGES=$$(docker images | grep "planet/" | awk '{print $$3}') ;\
	if [ ! -z "$$DEADIMAGES" ] ; then \
		docker rmi -f $$DEADIMAGES ;\
	fi

.PHONY: get-version
get-version:
	@echo $(PLANET_BUILD_TAG)
