.PHONY: all


HAPROXY_URL ?= http://www.haproxy.org/download/2.4/src/haproxy-2.4.2.tar.gz
HAPROXY_SRC_DIR := /opt/haproxy
CURL ?= curl -L --retry 5
MAKE_OPTS := TARGET=linux-glibc USE_GETADDRINFO=1 USE_OPENSSL=1 USE_PCRE2=1 USE_PCRE2_JIT=1 USE_PROMEX=1 USE_SYSTEMD=1 EXTRA_OBJS=""


all: build
	@echo "\\n---> Building and installing HAProxy:\\n"
	mkdir -p /usr/local/etc/haproxy
	cp -R /usr/src/haproxy/examples/errorfiles /usr/local/etc/haproxy/errors


build:
	mkdir -p $(HAPROXY_SRC_DIR)
	$(CURL) $(SRC_URL) -o haproxy.tar.gz
	tar -xzvf haproxy.tar.gz -C $(HAPROXY_SRC_DIR)
	cd $(HAPROXY_SRC_DIR)
	make all $(MAKE_OPTS)
	make install-bin $(MAKE_OPTS)

