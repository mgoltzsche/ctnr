BUILDIMAGE=local/cntnr-build:latest
DOCKERRUN=docker run -v "${REPODIR}:/work" -w /work -u `id -u`:`id -g`

REPODIR=$(shell pwd)
GOPATH=${REPODIR}/build
PKGNAME=github.com/mgoltzsche/cntnr
VENDORLOCK=${REPODIR}/vendor/ready
BINARY=cntnr

BUILDTAGS?=containers_image_ostree_stub containers_image_storage_stub containers_image_openpgp libdm_no_deferred_remove btrfs_noversion
BUILDTAGS_STATIC=${BUILDTAGS} linux static_build exclude_graphdriver_devicemapper
LDFLAGS_STATIC=${LDFLAGS} -extldflags '-static'

CNI_VERSION=0.6.0
CNIGOPATH=${GOPATH}/cni


all: build-static cni-plugins-static

build-static: buildimage
	${DOCKERRUN} ${BUILDIMAGE} make build BUILDTAGS="${BUILDTAGS_STATIC}" LDFLAGS="${LDFLAGS_STATIC}"

build: dependencies
	# Build application
	GOPATH="${GOPATH}" \
	go build -o dist/bin/${BINARY} -a -ldflags "${LDFLAGS}" -tags "${BUILDTAGS}" "${PKGNAME}"

test: dependencies
	# Run tests. TODO: more tests
	GOPATH="${GOPATH}" go test -tags "${BUILDTAGS}" "${PKGNAME}/model"

runc: dependencies
	rm -rf "${GOPATH}/src/github.com/opencontainers/runc"
	mkdir -p "${GOPATH}/src/github.com/opencontainers"
	cp -r "${GOPATH}/src/${PKGNAME}/vendor/github.com/opencontainers/runc" "${GOPATH}/src/github.com/opencontainers/runc"
	ln -s "${REPODIR}/vendor" "${GOPATH}/src/github.com/opencontainers/runc/vendor"
	cd "${GOPATH}/src/github.com/opencontainers/runc" && \
	export GOPATH="${GOPATH}" && \
	make clean && \
	make BUILDTAGS='seccomp selinux ambient' && \
	cp runc "${REPODIR}/dist/bin/runc"

cni-plugins-static: buildimage
	${DOCKERRUN} ${BUILDIMAGE} make cni-plugins LDFLAGS="${LDFLAGS_STATIC}"

cni-plugins:
	# Build CNI plugins
	mkdir -p "${CNIGOPATH}"
	wget -O "${CNIGOPATH}/cni-${CNI_VERSION}.tar.gz" "https://github.com/containernetworking/cni/archive/v${CNI_VERSION}.tar.gz"
	wget -O "${CNIGOPATH}/cni-plugins-${CNI_VERSION}.tar.gz" "https://github.com/containernetworking/plugins/archive/v${CNI_VERSION}.tar.gz"
	tar -xzf "${CNIGOPATH}/cni-${CNI_VERSION}.tar.gz" -C "${CNIGOPATH}"
	tar -xzf "${CNIGOPATH}/cni-plugins-${CNI_VERSION}.tar.gz" -C "${CNIGOPATH}"
	rm -rf "${CNIGOPATH}/src/github.com/containernetworking"
	mkdir -p "${CNIGOPATH}/src/github.com/containernetworking"
	mv "${CNIGOPATH}/cni-${CNI_VERSION}"     "${CNIGOPATH}/src/github.com/containernetworking/cni"
	mv "${CNIGOPATH}/plugins-${CNI_VERSION}" "${CNIGOPATH}/src/github.com/containernetworking/plugins"
	export GOPATH="${CNIGOPATH}" && \
	for TYPE in main ipam meta; do \
		for CNIPLUGIN in `ls ${CNIGOPATH}/src/github.com/containernetworking/plugins/plugins/$$TYPE`; do \
			(set -x; go build -o dist/cni-plugins/$$CNIPLUGIN -a -ldflags "${LDFLAGS}" github.com/containernetworking/plugins/plugins/$$TYPE/$$CNIPLUGIN) || exit 1; \
		done \
	done

buildimage:
	docker build -t ${BUILDIMAGE} .

build-sh: buildimage
	${DOCKERRUN} -ti ${BUILDIMAGE} /bin/sh

dependencies: .workspace
	# Fetch dependencies
	[ "`ls vendor`" ] || \
		(GOPATH="${GOPATH}" go get github.com/LK4D4/vndr && \
		cd "${GOPATH}/src/${PKGNAME}" && "${GOPATH}/bin/vndr" -whitelist='.*')

.workspace:
	# Prepare workspace directory
	[ -d "${GOPATH}" ] || mkdir -p vendor ${GOPATH}/src/${PKGNAME} && \
		ln -sf ${REPODIR}/* ${GOPATH}/src/${PKGNAME} && \
		ln -sf ${REPODIR}/vendor.conf ${GOPATH}/vendor.conf && \
		rm -f ${GOPATH}/src/${PKGNAME}/build

install:
	cp dist/bin/cntnr /bin/cntnr

clean:
	rm -rf ./build ./dist
