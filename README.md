# cntnr

CNTNR DEVELOPMENT IS IN AN EARLY STATE!

cntnr is a container engine library and CLI built on top of [runc](https://github.com/opencontainers/runc)
to manage and build OCI images as well as runtime bundles.  
cntnr aims to ease system container creation and execution as unprivileged user.
Besides cntnr is a platform to try out new runc features.


## Features
- OCI bundle and container preparation as well as execution as unprivileged user
- OCI image build as unprivileged user
- Simple concurrently accessible portable POSIX-based image and bundle store
- Image and bundle file system creation using [umoci](https://github.com/openSUSE/umoci)
- Various image formats and transports supported by [containers/image](https://github.com/containers/image)
- Optional container networking using [CNI](https://github.com/containernetworking/cni) (as OCI runtime hook)
- Partial [docker compose](https://docs.docker.com/compose/compose-file/) file format support
- Simple CLI partially compatible with [docker](https://www.docker.com/)'s
- Easy installation: single statically linked binary (plus optional CNI plugin binaries) and convention over configuration


## Rootless containers

Concerning accessibility, usability and security container engines that do not require root privileges have several advantages compared to those that do:
- **Containers can be run by unprivileged users.**  
  _Required in restrictive environments and useful for graphical applications._
- **Container images can be built everywhere.**  
  _Higher flexibility in unprivileged CI/CD build jobs - running a container in a container will soon also be possible._
- **A higher degree and more flexible level of security.**  
  _Less likely for an attacker to gain root access through a possible engine security leak when run as unprivileged user._  
  _User/group-based container access control leveraging the host OS' ACL._

See [Aleksa Sarai's blog post](https://www.cyphar.com/blog/post/rootless-containers-with-runc) (which inspired me to start this project) for more information.


### Limitations & challenges

Container execution as unprivileged user is limited:


**Container networks cannot be configured.**
As a result in a restrictive environment without root access only the host network can be used.
A feature on the roadmap is a daemon that runs as root and can configure a separate container namespace for an unprivileged user's container.


**Inside the container a process' or file's user cannot be changed.**
This is caused by the fact that all operations in the container are still run by the host user who is mapped to a user inside the container.
Unfortunately this stops some official docker images as well as package managers from working.
A solution approach is to hook the corresponding system calls and simulate them without actually doing them.
Though this does not solve the whole problem since some applications may still not work when they ask for the actual state while expecting their earlier system calls to be applied. For this reason e.g. apt-get cannot be used when the container is run as unprivileged user. Fortunately this approach works for dnf and yum as well as apk which is why for instance alpine-based images can already be built by unprivileged users.  
(=> See https://github.com/dex4er/fakechroot/wiki)


## Build

Build the binary `dist/bin/cntnr` as well as `dist/bin/cni-plugins` on a Linux machine with git, make and docker:
```
git clone https://github.com/mgoltzsche/cntnr.git
cd cntnr
make
```  
Install in `/usr/local`:
```
sudo make install
```  
Optionally the project can now be opened with LiteIDE running in a cntnr container  
_(Please note that it takes some time to build the LiteIDE container image)_:
```
make ide
```


## Examples

### Create and run container from Docker image
```
> cntnr run docker://alpine:3.6 echo hello world
hello world
```

### Create and run Firefox as unprivileged user
Build a Firefox ESR container image `local/firefox:alpine`:
```
cntnr image create \
	--from=docker://alpine:3.7 \
	--author='John Doe' \
	--run='apk add --update --no-cache firefox-esr libcanberra-gtk3 adwaita-icon-theme ttf-ubuntu-font-family' \
	--cmd=firefox \
	--tag=local/firefox:alpine
```  

Create a bundle named `firefox` from the previously built image (the `--update` option makes this operation idempotent):
```
cntnr bundle create -b firefox --update=true \
	--env DISPLAY=$DISPLAY \
	--mount /tmp/.X11-unix:/tmp/.X11-unix \
	--mount /etc/machine-id:/etc/machine-id:ro \
	local/firefox:alpine
```  

Run the previously prepared `firefox` bundle as container:
```
cntnr bundle run firefox
```


## Related tools

- [docker](https://www.docker.com/)
- [rkt](https://rkt.io)
- [runc](https://github.com/opencontainers/runc), [skopeo](https://github.com/projectatomic/skopeo), [umoci](https://github.com/openSUSE/umoci)
- [udocker](https://github.com/indigo-dc/udocker)
- [singularity](http://singularity.lbl.gov/)

## Roadmap

- separate OCI hook binary
- CLI improvements: image rm, image build, bundle run, compose
- additional configurable read-only image stores
- Improved container, bundle and image garbage collection
- health check
- systemd integration (cgroup, startup notification)
- network manager daemon with ACL to be used by unprivileged users to configure their container networks
- service discovery integration (hook / DNS; consul, etcd)

## TL;DR
Experiments with nested containers on an ubuntu 16.04 host
```
# Working example (privileged docker host container):
docker run -ti --privileged --name cnestedpriv --rm \
	-v $(pwd)/dist/bin/cntnr:/bin/cntnr \
	ubuntu:16.04
> cntnr run -t --network=host docker://alpine:3.7

# Not working (unprivileged docker host container):
docker run -ti --name cnested --cap-add=SYS_PTRACE --rm \
	-v $(pwd)/dist/bin/cntnr:/bin/cntnr \
	-v /boot/config-4.4.0-104-generic:/boot/config-4.4.0-104-generic \
	alpine:3.7
> cntnr run  -ti --rootless --net=host docker://alpine:3.7
=> doesn't work
> apt-get update
> apt-get install -y wget ptrace apparmor
> wget -O /bin/checkconfig https://raw.githubusercontent.com/moby/moby/master/contrib/check-config.sh && chmod +x /bin/checkconfig
> checkconfig # docker host check script
=> all general features are fine
> cntnr run -ti --network=host docker://alpine:3.7
=> error: "WARN[0000] os: process already finished" and then terminates with "running exec setns process for init caused \"exit status 34\""
=> capability (or syscall in seccomp) missing

# Works in privileged cntnr container (as root or unprivileged user)
dist/bin/cntnr bundle create -b nested --update -t \
	--cap-add=all \
	--mount=./dist/bin/cntnr:/bin/cntnr:exec:ro \
	--mount=./dist/cni-plugins:/cni \
	--mount=/boot/config-4.4.0-104-generic:/boot/config-4.4.0-104-generic \
	docker://alpine:3.7
dist/bin/cntnr bundle run nested
> cntnr run -t --rootless --network=host docker://alpine:3.7

# Problem remaining: Works in privileged docker container but not in unprivileged:
# => Look for forbidden capabilities/syscalls that can be avoided or must be added to the outer container
# => support low child container isolation to be able to build an image everywhere

# Known errors and workarounds to run a container as unprivileged user (also see https://github.com/opencontainers/runc/issues/1456):
"running exec setns process for init caused \"exit status 34\""
  -> inner container: add --rootless option (if that has no effect: add setns syscall to list of SCMP_ACT_ALLOW calls (TODO: which syscall exactly?))
  -> {root} (outer container: add --seccomp=unconfined option instead of --cap-add=all)
"mkdir /sys/fs/cgroup/cpuset/05dh[...]: permission denied"
  -> inner container: add --rootless option
"could not create session key: operation not permitted"
  -> inner container: set --no-new-keyring libcontainer run option (TODO: expose)
  -> outer container: allow corresponding syscall in seccomp profile (dirty: set --seccomp=unconfined)
"pivot_root operation not permitted"
  -> outer container: seccomp: add "pivot_root" syscall to the list of SCMP_ACT_ALLOW calls
  -> inner container: set --no-pivot libcontainer run option (TODO: expose)

# Deprecated: Different error in rootless container:
cntnr bundle create -b nested-alpine --update -t --cap-add=all --seccomp=unconfined --mount=./dist/bin/cntnr:/bin/cntnr:exec:ro --mount=./dist/cni-plugins:/cni --mount=/boot/config-4.4.0-104-generic:/boot/config-4.4.0-104-generic docker://alpine:3.7
=> /sys/fs/cgroup is there (of type 'none' and with option 'rbind' in opposite to the root container mount of type 'sysfs')
cntnr bundle run nested-alpine
> cntnr run -t --network=host docker://alpine:3.7
=> error: "mkdir /sys/fs/cgroup/cpuset/pqf3hvfjl5cnlpk4hvdbsievki: permission denied"
=> problem: other cgroups should stay untouched but only child cgroups required for rw access:
    https://github.com/opencontainers/runtime-spec/issues/66
    https://github.com/opencontainers/runtime-spec/pull/397
   => seems not to be completely solvable as long as one has to deal with kernel <4.6
    https://github.com/opencontainers/runc/issues/225
```