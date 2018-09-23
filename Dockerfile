FROM golang:alpine3.7 AS cntnr-build
RUN apk add --update --no-cache gcc musl-dev libseccomp-dev btrfs-progs-dev lvm2-dev make git

FROM fedora:28 as proot
RUN dnf update -y \
	&& dnf install -y make gcc gcc-c++ glibc-devel glibc-static libstdc++-static libattr-devel libseccomp-devel protobuf-devel curl python \
	&& (dnf install -y git || true)
ARG TALLOC_VERSION=2.1.8
RUN curl -LOk https://www.samba.org/ftp/talloc/talloc-${TALLOC_VERSION}.tar.gz \
	&& tar zxvf talloc-${TALLOC_VERSION}.tar.gz \
	&& cd talloc-${TALLOC_VERSION} \
	&& ./configure --without-gettext --prefix=/usr \
	&& make install \
	&& ar rcs /usr/local/lib64/libtalloc.a bin/default/talloc*.o \
	&& rm -rf talloc-${TALLOC_VERSION}*
ARG PROTOBUFC_VERSION=1.3.1
RUN curl -LOk https://github.com/protobuf-c/protobuf-c/releases/download/v${PROTOBUFC_VERSION}/protobuf-c-${PROTOBUFC_VERSION}.tar.gz \
	&& tar zxvf protobuf-c-${PROTOBUFC_VERSION}.tar.gz --no-same-owner \
	&& cd protobuf-c-${PROTOBUFC_VERSION} \
	&& ./configure --prefix=/usr && make && make install \
	&& rm -rf protobuf-c-${PROTOBUFC_VERSION}*
ARG PROOT_VERSION=081bb63955eb4378e53cf4d0eb0ed0d3222bf66e
RUN git clone https://github.com/rootless-containers/PRoot.git \
	&& cd PRoot \
	&& git checkout ${PROOT_VERSION} \
	&& sed -Ei '/^#include <attr\/xattr.h>$/a#ifndef ENOATTR\n# define ENOATTR ENODATA\n#endif' src/extension/fake_id0/fake_id0.c
WORKDIR /PRoot/src
ENV PKG_CONFIG_PATH=/usr/lib/pkgconfig
RUN make && mv proot / && make clean