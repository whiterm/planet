.PHONY: all pull-from-internet

IMAGE:=docker.io/haproxy:2.4.1-alpine
# OUTDIR defines the output directory for the resulting tarball
# (set in the parent makefile)
override OUT:=$(OUTDIR)/haproxy.tar.gz

all: pull-from-internet $(OUT)

$(OUT): haproxy.mk
	@echo "Exporting image to file system..."
	docker save -o $@ $(IMAGE)

pull-from-internet:
	@echo "Pulling docker image..."
	docker pull $(IMAGE)
