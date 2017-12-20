FROM golang:alpine3.7
MAINTAINER Max Goltzsche <max.goltzsche@gmail.com>

RUN apk --update --no-cache add gcc musl-dev libseccomp-dev btrfs-progs-dev lvm2-dev make git
