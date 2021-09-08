ARG PLANET_OS_IMAGE=planet/os
FROM $PLANET_OS_IMAGE

ARG SECCOMP_VER
ARG IPTABLES_VER
ARG PLANET_UID
ARG PLANET_GID

# FIXME: allowing downgrades and pinning the version of libip4tc0 for iptables
# as the package has a dependency on the older version as the one available.
RUN export DEBIAN_FRONTEND=noninteractive && set -ex && \
        apt-get update && \
        apt-get install -q -y --allow-downgrades --no-install-recommends \
        bridge-utils \
        seccomp=$SECCOMP_VER \
        bash-completion \
        kmod \
        libip4tc0=1.6.0+snapshot20161117-6 \
        ebtables \
        libsqlite3-0 \
        libpcre2-dev \
        e2fsprogs \
        libncurses5 \
        net-tools \
        curl \
        iproute2 \
        lsb-base \
        dash \
        ca-certificates \
        aufs-tools \
        xfsprogs \
        dbus \
        dnsutils \
        ethtool \
        sysstat \
        nano \
        vim \
        iotop \
        htop \
        ifstat \
        iftop \
        traceroute \
        tcpdump \
        procps \
        coreutils \
        lsof \
        socat \
        nmap \
        netcat \
        nfs-common \
        jq \
        conntrack \
        open-iscsi \
        strace \
        netbase \
        bsdmainutils \
        && apt-get -y autoclean && apt-get -y clean && apt-get autoremove \
        && rm -rf /var/lib/apt/lists/*;

# We need to use a newer version of iptables than debian has available
# not ideal, but it's easier to run `make install` if we run this inline instead of a multi-stage build
RUN export DEBIAN_FRONTEND=noninteractive && set -ex \
        && apt-get update \
        && apt-get install -q -y --allow-downgrades --no-install-recommends \
        git \
        autoconf \
        libtool \
        automake \
        pkg-config \
        libmnl-dev \
        make \
        && mkdir /tmp/iptables.build \
        && git clone git://git.netfilter.org/iptables.git --branch ${IPTABLES_VER} --single-branch /tmp/iptables.build \
        && cd /tmp/iptables.build \
        && ./autogen.sh \
        && ./configure --disable-nftables \
        && make \
        && make install \
        && apt-get remove -y \
        git \
        autoconf \
        libtool \
        automake \
        pkg-config \
        libmnl-dev \
        make \
        && apt-get -y autoclean && apt-get -y clean && apt-get autoremove -y \
        && rm -rf /var/lib/apt/lists/*;

RUN set -ex && \
    groupadd --system --non-unique --gid ${PLANET_GID} planet && \
    useradd --system --non-unique --no-create-home -g ${PLANET_GID} -u ${PLANET_UID} planet && \
    groupadd --system docker && \
    usermod -a -G planet root && \
    usermod -a -G docker planet;
