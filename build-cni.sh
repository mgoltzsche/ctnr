#!/bin/sh

CNI_VERSION=${CNI_VERSION:-0.6.0-rc1}

REPOPATH="$(dirname "$0")"
REPOPATH="$(cd "$REPOPATH" && pwd)"
cd "$REPOPATH"
export GOPATH="$REPOPATH/build/cni"

# Fetch CNI + plugins
rm -rf "$GOPATH" &&
mkdir -p "$GOPATH" &&
curl -fSL -o "$GOPATH/cni-$CNI_VERSION.tar.gz"         "https://github.com/containernetworking/cni/archive/v$CNI_VERSION.tar.gz" &&
curl -fSL -o "$GOPATH/cni-plugins-$CNI_VERSION.tar.gz" "https://github.com/containernetworking/plugins/archive/v$CNI_VERSION.tar.gz" &&
tar -xzf "$GOPATH/cni-$CNI_VERSION.tar.gz"         -C "$GOPATH" &&
tar -xzf "$GOPATH/cni-plugins-$CNI_VERSION.tar.gz" -C "$GOPATH" &&
mkdir -p "$GOPATH/src/github.com/containernetworking" &&
mv "$GOPATH/cni-$CNI_VERSION"     "$GOPATH/src/github.com/containernetworking/cni" &&
mv "$GOPATH/plugins-$CNI_VERSION" "$GOPATH/src/github.com/containernetworking/plugins" &&

# Build CNI
go build -o dist/bin/cnitool github.com/containernetworking/cni/cnitool || exit 1
BUILT=cnitool

# Build CNI plugins
for TYPE in ipam main meta; do
	for CNI_PLUGIN in $(ls "$GOPATH/src/github.com/containernetworking/plugins/plugins/$TYPE/"); do
		go build -o dist/cni-plugins/$CNI_PLUGIN github.com/containernetworking/plugins/plugins/$TYPE/$CNI_PLUGIN || exit 1
		BUILT="$BUILT, $CNI_PLUGIN"
	done
done

echo "Successfully built $BUILT"
