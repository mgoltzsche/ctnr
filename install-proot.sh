#!/bin/sh

##
# This script downloads and installs a static PRoot binary.
# PRoot originates from https://github.com/proot-me/PRoot
# and is licenced under GPL-2.0.
##

curl -fSL https://github.com/proot-me/proot-static-build/releases/download/v5.1.1/proot_5.1.1_x86_64_rc2--no-seccomp > /usr/local/bin/proot
