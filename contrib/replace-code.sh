#!/bin/sh

[ $# -eq 2 ] || (echo "Usage: $0 PATTERN REPLACESTR"; false) || exit 1

escExpr() {
	echo "$1" | sed 's/\//\\\//g'
}

find . -type f -not \( -path './.git/*' -o -path './vendor/*' -o -path './build/*' \) \
	-exec sed -i "s/$(escExpr "$1")/$(escExpr "$2")/g" {} +
