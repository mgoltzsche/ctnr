# cntnr

CNTNR DEVELOPMENT IS IN A VERY EARLY STATE!

cntnr is a lightweight container engine library and CLI built on top of [runc](https://github.com/opencontainers/runc). It manages and builds images as well as OCI runtime bundles. It is a platform to try out runc technologies.
cntnr aims to ease system container creation and execution as unprivileged user as well as the integration with other tools such as [systemd](https://www.freedesktop.org/wiki/Software/systemd/) and [consul](https://www.consul.io/).

Multiple containers can be composed and run with a single CLI command.
The [docker compose](https://docs.docker.com/compose/compose-file/) file format is also supported.
Images can be fetched and converted from various sources using the [containers/image](https://github.com/containers/image) library.
Container networks are managed in OCI hooks using [CNI](https://github.com/containernetworking/cni) and its [plugins](https://github.com/containernetworking/plugins).


## Rootless containers

The ability to run a container as unprivileged user has some advantages:

- Container images can be built inside an unprivileged container.
- A container can be run in a restrictive, rootless environment.
- A higher degree and more flexible level of security since you can rely on your host OS' ACL.

See [Aleksa Sarai's blog post](https://www.cyphar.com/blog/post/rootless-containers-with-runc) (which inspired me to start this project) for more information.


### Limitations & challenges

Container execution as unprivileged user is limited:


**A separate container network namespace cannot be configured.**
As a result in a restrictive environment without root access only the host network can be used.
A feature on the roadmap is a daemon that runs as root and can configure a separate container namespace for an unprivileged user's container.


**Inside the container a process' or file's user cannot be changed.**
This is caused by the fact that all operations in the container are still run by the host user whose ID is simply mapped to 0/root inside the container.
Unfortunately this stops many official docker images as well as package managers from working.
A solution approach is to hook the corresponding system calls and simulate them without actually doing them.
Though this does not solve the whole problem since some applications may still not work when they ask for the actual state and expect their earlier system calls to be applied. For this reason e.g. apt-get cannot be used when the container is run as unprivileged user. Fortunately this approach works for yum and also for apk which is why for instance alpine-based images can already be built by unprivileged users.


## Build
Build the binary dist/bin/cntnr
```
./build.sh
```


## Examples

### Create and run container from Docker image
```
> cntnr run docker://alpine:3.6 echo hello world
hello world
```

### Create and run Firefox as unprivileged user
Build a Firefox ESR container image tagged as `local/firefox:alpine`:
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


## Roadmap

- separate OCI hook binary
- CLI improvements: image build, bundle run, compose create
- image/bundle store locks
- additional configurable read-only image stores
- container, bundle and image garbage collection
- apply CLI/compose network configuration
- health check
- systemd integration (notify when startup complete)
- service discovery integration (consul, etcd)
- network manager daemon with ACL to be used by unprivileged users to configure their container networks
- container annotation driven env var sync with distributed KV store (consul, etcd) to e.g. auto-configure webserver/loadbalancer or for basic master election