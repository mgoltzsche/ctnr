#!/bin/sh

SCRIPTDIR="$(dirname "$0")" &&
cd "$SCRIPTDIR/.." &&
egrep -nEiR "$1" . 2>/dev/null | grep -Ev '(^| )\./(build|vendor|dist|cntnr).*'