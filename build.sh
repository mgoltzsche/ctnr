#!/bin/sh

# Go 1.8+ required. Ubuntu installation:
#  sudo add-apt-repository ppa:longsleep/golang-backports
#  sudo apt-get update
#  sudo apt-get install golang-go

usage() {
	echo "Usage: %0 install|run" >&2
	exit 1
}

[ $# -eq 0 -o $# -eq 1 ] || usage

REPOPATH="$(dirname "$0")"
REPOPATH="$(cd "$REPOPATH" && pwd)"
PKGNAME=github.com/mgoltzsche/cntnr
MAIN=$PKGNAME/cmd/cntnr
BINARY=cntnr

case "$1" in
	install|'')
		# Exclude ostree since not available on ubuntu 16.04
		BUILDTAGS=containers_image_ostree_stub
		(
		set -x

		# Format code
		(find . -mindepth 1 -maxdepth 1 -type d; ls *.go) | grep -Ev '^(./build.*|./.git|./bin)$' | xargs -n1 -I'{}' gofmt -w "$REPOPATH/{}" &&

		# Create workspace
		export GOPATH="$REPOPATH/build" &&
		#export GO15VENDOREXPERIMENT=1 &&
		mkdir -p "$GOPATH/src/$PKGNAME" &&
		ln -sf "$REPOPATH"/* "$GOPATH/src/$PKGNAME" &&
		ln -sf "$REPOPATH/vendor.conf" "$GOPATH/vendor.conf" &&
		rm -f "$GOPATH/src/$PKGNAME/build" || exit 1

		# Fetch dependencies
		if [ ! -f ./build/vndr ]; then
			# Fetch and build dependency manager
			VNDR="$GOPATH/vndr"
			go get github.com/LK4D4/vndr &&
			go build -o "$VNDR" github.com/LK4D4/vndr &&
			# Fetch dependencies
			(
				cd "$GOPATH/src/$PKGNAME" &&
				"$VNDR" -whitelist='.*'
			) || exit 1
		fi

		# Build cntnr binary
		go build -o dist/bin/$BINARY -tags "$BUILDTAGS" $PKGNAME/cmd/cntnr &&

		# Build and run tests
		go test -tags "$BUILDTAGS" $PKGNAME/model
		) || exit 1

		echo "$BINARY has been built successfully!"
	;;
	run)
		set -x
		"$REPOPATH/dist/bin/$BINARY" -verbose=true -name=examplepod -uuid-file=/var/run/examplepod.uuid run test-resources/example-docker-compose-images.yml
	;;
	*)
		usage
	;;
esac
