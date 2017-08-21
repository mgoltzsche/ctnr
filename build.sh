#!/bin/sh

# Go 1.8+ required. Ubuntu installation:
#  sudo add-apt-repository ppa:longsleep/golang-backports
#  sudo apt-get update
#  sudo apt-get install golang-go

usage() {
	echo "Usage: %0 install|run" >&2
	exit 1
}

REPOPATH="$(dirname "$0")"
REPOPATH="$(cd "$REPOPATH" && pwd)"
PKGNAME=github.com/mgoltzsche/cntnr
MAIN=$PKGNAME/cmd/cntnr
BINARY=cntnr

# Exclude ostree since not available on ubuntu 16.04
BUILDTAGS=containers_image_ostree_stub

initWorkspace() {
	# Format code
	(find . -mindepth 1 -maxdepth 1 -type d; ls *.go) | grep -Ev '^(./build.*|./.git|./bin)$' | xargs -n1 -I'{}' gofmt -w "$REPOPATH/{}" &&

	# Create workspace
	export GOPATH="$REPOPATH/build" &&
	#export GO15VENDOREXPERIMENT=1 &&
	mkdir -p "$GOPATH/src/$PKGNAME" &&
	ln -sf "$REPOPATH"/* "$GOPATH/src/$PKGNAME" &&
	ln -sf "$REPOPATH/vendor.conf" "$GOPATH/vendor.conf" &&
	rm -f "$GOPATH/src/$PKGNAME/build" || return 1

	# Fetch dependencies
	VNDR="$GOPATH/bin/vndr"
	if [ ! -f "$VNDR" ]; then
		# Fetch and build dependency manager
		go get github.com/LK4D4/vndr &&
		# Fetch dependencies
		(
			cd "$GOPATH/src/$PKGNAME" &&
			"$VNDR" -whitelist='.*'
		) || return 1
	fi
}

case "$1" in
	install|'')
		[ $# -eq 0 ] || usage
		(
		set -x

		initWorkspace &&

		# Build cntnr binary
		go build -o dist/bin/$BINARY -tags "$BUILDTAGS" $PKGNAME &&

		# Build and run tests
		go test -tags "$BUILDTAGS" $PKGNAME/model
		) || exit 1

		echo "$BINARY has been built successfully!"
	;;
	cobra)
		shift &&
		initWorkspace &&
		COBRA="$GOPATH/bin/cobra"
		if [ ! -f "$COBRA" ]; then
			# Fetch and build cobra CLI
			go get github.com/spf13/cobra/cobra || exit 1
		fi
		export GOPATH="$REPOPATH"
		"$COBRA" "$@"
	;;
	run)
		[ $# -eq 0 ] || usage
		set -x
		"$REPOPATH/dist/bin/$BINARY" -verbose=true -name=examplepod -uuid-file=/var/run/examplepod.uuid run test-resources/example-docker-compose-images.yml
	;;
	*)
		usage
	;;
esac
