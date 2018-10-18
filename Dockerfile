FROM golang:alpine3.8 AS ctnr-build
RUN apk add --update --no-cache gcc musl-dev libseccomp-dev btrfs-progs-dev lvm2-dev make git

FROM fedora:28 as proot
RUN dnf install -y make gcc gcc-c++ glibc-devel glibc-static libstdc++-static libattr-devel libseccomp-devel protobuf-devel curl python \
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
ARG PROOT_VERSION=f4dc8cb6f5f31beda5f69f0d476a3196d31c4336
RUN git clone https://github.com/rootless-containers/PRoot.git \
	&& cd PRoot \
	&& git checkout ${PROOT_VERSION}
WORKDIR /PRoot/src
ENV PKG_CONFIG_PATH=/usr/lib/pkgconfig
RUN make && mv proot / && make clean

FROM ctnr-build AS liteide
ARG LITEIDE_PKGS="g++ qt5-qttools qt5-qtbase-dev qt5-qtbase-x11 qt5-qtwebkit xkeyboard-config libcanberra-gtk3 adwaita-icon-theme ttf-dejavu"
RUN apk add --update --no-cache ${LITEIDE_PKGS} || /usr/lib/qt5/bin/qmake -help >/dev/null
RUN git clone https://github.com/visualfc/liteide.git \
	&& cd liteide/build \
	&& ./update_pkg.sh && QTDIR=/usr/lib/qt5 ./build_linux.sh \
	&& rm -rf /usr/local/bin \
	&& ln -s `pwd`/liteide/bin /usr/local/bin