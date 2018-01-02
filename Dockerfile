FROM golang:alpine3.7
MAINTAINER Max Goltzsche <max.goltzsche@gmail.com>

RUN apk add --update --no-cache gcc musl-dev libseccomp-dev btrfs-progs-dev lvm2-dev make git
