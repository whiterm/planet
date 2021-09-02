.PHONY: all

HAPROXY_VERSION ?= 2.4.2
HAPROXY_SRC_URL ?= http://www.haproxy.org/download/2.4/src/haproxy-$(HAPROXY_VERSION).tar.gz
HAPROXY_SRC_DIR := $(ASSETDIR)/haproxy-$(HAPROXY_VERSION)
CURL ?= curl -L --retry 5
MAKE_OPTS := TARGET=linux-glibc USE_GETADDRINFO=1 USE_OPENSSL=1 USE_PCRE2=1 USE_PCRE2_JIT=1 USE_PROMEX=1 USE_SYSTEMD=1 EXTRA_OBJS=""


all: build
	@echo "\\n---> Building and installing HAProxy:\\n"
	mkdir -p $(ROOTFS)/usr/local/etc/haproxy $(ROOTFS)/usr/local/sbin/ $(ROOTFS)/etc/haproxy/certs
	cp -R $(HAPROXY_SRC_DIR)/examples/errorfiles $(ROOTFS)/usr/local/etc/haproxy/errors
	cp -R $(HAPROXY_SRC_DIR)/haproxy $(ROOTFS)/usr/local/sbin/
	cp -af ./haproxy.service $(ROOTFS)/lib/systemd/system
	#smoke test
	$(ROOTFS)/usr/local/sbin/haproxy -v


build:
	mkdir -p $(HAPROXY_SRC_DIR)
	$(CURL) $(HAPROXY_SRC_URL) -o $(ASSETDIR)/haproxy-$(HAPROXY_VERSION).tar.gz
	tar -xzvf $(ASSETDIR)/haproxy-$(HAPROXY_VERSION).tar.gz -C $(HAPROXY_SRC_DIR) --strip-components=1
	make -C $(HAPROXY_SRC_DIR) -j $(shell nproc) all $(MAKE_OPTS)


