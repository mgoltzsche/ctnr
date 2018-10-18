#!/bin/sh

SCRIPTDIR="$(dirname "$0")" &&
cd "$SCRIPTDIR/.." &&
egrep -nER "$1" . 2>/dev/null | grep -Ev '(^| )\./(build|vendor|dist|ctnr).*'
