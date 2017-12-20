FROM ubuntu:16.04
MAINTAINER Max Goltzsche <max.goltzsche@gmail.com>

RUN set -x \
	&& apt-get update \
	&& apt-get install -y software-properties-common \
	&& add-apt-repository ppa:longsleep/golang-backports \
	&& apt-get update

RUN apt-get install -y golang-go libseccomp-dev libgpgme11-dev libassuan-dev btrfs-tools libdevmapper-dev curl
