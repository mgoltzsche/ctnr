# cntnr

CNTNR DEVELOPMENT IS IN AN EARLY STATE!

cntnr is a CLI built on top of [runc](https://github.com/opencontainers/runc)
to manage and build OCI images as well as containers.  
cntnr aims to ease system container creation and execution as unprivileged user.  
Also cntnr is a platform to try out new runc features.


## Features
- OCI bundle and container preparation as well as execution as unprivileged user using [runc](https://github.com/opencontainers/runc)
- OCI image build as unprivileged user
- Simple concurrently accessible image and bundle store
- Image and bundle file system creation (based on [umoci](https://github.com/openSUSE/umoci))
- Various image formats and transports supported by [containers/image](https://github.com/containers/image)
- Container networking using [CNI](https://github.com/containernetworking/cni) (optional, requires root, as OCI runtime hook)
- [Dockerfile](https://docs.docker.com/engine/reference/builder/) support
- [Docker Compose 3](https://docs.docker.com/compose/compose-file/) support (subset) using [docker/cli](https://github.com/docker/cli/)
- Easy to learn: [docker](https://www.docker.com/)-like CLI
- Easy installation: single statically linked binary (plus optional binaries: CNI plugins, proot) and convention over configuration


## Rootless containers

Concerning accessibility, usability and security a rootless container engine has several advantages:
- **Containers can be run by unprivileged users.**  
  _Required in restrictive environments and useful for graphical applications._
- **Container images can be built almost in every Linux environment.**  
  _More flexibility in unprivileged CI/CD builds - nesting unprivileged containers is still not working (see experiments below)._
- **A higher degree and more flexible level of security.**  
  _Less likely for an attacker to gain root access when run as unprivileged user._  
  _User/group-based container access control._


### Limitations & challenges

Container execution as unprivileged user is limited:


**Container networks cannot be configured.**
As a result in a restrictive environment without root access only the host network can be used.
(A planned workaround is to map ports consistently to higher free ranges on the host network and back using [PRoot](https://github.com/rootless-containers/PRoot)*)


**Inside the container a process' or file's user cannot be changed.**
This is caused by the fact that all operations in the container are still run by the host user (who is just mapped to user 0 inside the container).
Unfortunately this stops many package managers as well as official docker images from working:
While `apk` already works with plain [runc](https://github.com/opencontainers/runc) `apt-get` does not since it requires to change a user permanently.  
To overcome this limitation cntnr supports the `user.rootlesscontainers` xattr and integrates with [PRoot](https://github.com/rootless-containers/PRoot)*.  


For more details see Aleksa Sarai's [summary](https://rootlesscontaine.rs/) of the state of the art of rootless containers.


\* _[PRoot](https://github.com/rootless-containers/PRoot) is a binary that hooks its child processes' kernel-space system calls using `ptrace` to simulate them in the user-space. This is more reliable but slower than hooking libc calls using `LD_PRELOAD` as [fakechroot](https://github.com/dex4er/fakechroot) does it._  


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

The following examples assume your policy accepts docker images or you have copied [policy-example.json](policy-example.json) to `/etc/containers/policy.json` on your host.

### Create and run container from Docker image
```
$ cntnr run docker://alpine:3.8 echo hello world
hello world
```

### Create and run Firefox as unprivileged user
Build a Firefox ESR container image `local/firefox:alpine` (cached operation):
```
$ cntnr image create \
	--from=docker://alpine:3.8 \
	--author='John Doe' \
	--run='apk add --update --no-cache firefox-esr libcanberra-gtk3 adwaita-icon-theme ttf-ubuntu-font-family' \
	--cmd=firefox \
	--tag=local/firefox:alpine
```  

Create and run a bundle named `firefox` from the previously built image:
```
$ cntnr run -b firefox --update \
	--env DISPLAY=$DISPLAY \
	--mount src=/tmp/.X11-unix,dst=/tmp/.X11-unix \
	--mount src=/etc/machine-id,dst=/etc/machine-id,opt=ro \
	local/firefox:alpine
```  
_(Unfortunately tabs in firefox tend to crash)_
The `-b <BUNDLE>` and `--update` options make this operation idempotent:
The bundle's file system is reused and only recreated when the underlying image has changed.
Use these options to restart containers very quickly. Without them cntnr copies the
image file system on bundle creation which can take some time and disk space depending on the image's size.  
Also these options enable a container update on restart when the base image is frequently updated before the child image is rebuilt using the following command:
```
$ cntnr image import docker://alpine:3.8
```

### Build Dockerfile as unprivileged user
This example shows how to build a debian-based image with the help of [PRoot](https://github.com/rootless-containers/PRoot).

Dockerfile `Dockerfile-cowsay`:
```
FROM debian:9
RUN apt-get update && apt-get install -y cowsay
ENTRYPOINT ["/usr/games/cowsay"]
```
Build the image (Please note that this works only with `--proot` enabled. With plain cntnr/runc this doesn't work since `apt-get` fails to change uid/gid.):
```
$ cntnr image create --proot --dockerfile Dockerfile-cowsay --tag example/cowsay
```
Run a container using the previously built image (Please note that `--proot` is not required anymore):
```
$ cntnr run example/cowsay hello from container
 ______________________
< hello from container >
 ----------------------
        \   ^__^
         \  (oo)\_______
            (__)\       )\/\
                ||----w |
                ||     ||
```


## The OCI standard and this implementation

An *[OCI image](https://github.com/opencontainers/image-spec/tree/v1.0.0)* provides a base [configuration](https://github.com/opencontainers/image-spec/blob/v1.0.0/config.md) and file system to create an OCI bundle from. The file system consists of a list of layers represented by tar files each containing the diff to its predecessor.  
cntnr manages images in its local store directory in the [OCI image layout format](https://github.com/opencontainers/image-spec/blob/v1.0.0/image-layout.md).
Images are imported into the local store using the [containers/image](https://github.com/containers/image) library.
A new bundle is created by extracting the image's file system into a directory and [deriving](https://github.com/opencontainers/image-spec/blob/v1.0.0/conversion.md) the bundle's default configuration from the image's configuration plus user-defined options.


An *[OCI bundle](https://github.com/opencontainers/runtime-spec/blob/v1.0.0/bundle.md)* describes a container by
a [configuration](https://github.com/opencontainers/runtime-spec/blob/v1.0.0/config.md) and a file system.
Basically it is a directory containing a `config.json` file with the configuration and a sub directory with the root file system.  
cntnr manages bundles in its local store directory. Alternatively a custom directory can also be used as bundle.
OCI bundles generated by cntnr can also be run with other OCI-compliant container engines as [runc](https://github.com/opencontainers/runc/).


An *[OCI container](https://github.com/opencontainers/runtime-spec/blob/v1.0.0/runtime.md)* is a host-specific bundle instance.
On Linux it is a set of namespaces in which a configured process can be run.  
cntnr provides two wrapper implementations of the OCI runtime reference implementation
[runc/libcontainer](https://github.com/opencontainers/runc/blob/v1.0.0-rc5/libcontainer/README.md)
to either use an external runc binary or use libcontainer (no runtime dependencies!) controlled by a compiler flag.


## Related tools

- [cri-o](https://github.com/kubernetes-incubator/cri-o)
- [containerd](https://containerd.io/)
- [docker](https://www.docker.com/)
- [lxc](https://linuxcontainers.org/lxc/introduction/)
- [rkt](https://rkt.io)
- [rkt-compose](https://github.com/mgoltzsche/rkt-compose)
- [runc](https://github.com/opencontainers/runc)
- [runrootless](https://github.com/AkihiroSuda/runrootless)
- [singularity](http://singularity.lbl.gov/)
- [skopeo](https://github.com/projectatomic/skopeo), [umoci](https://github.com/openSUSE/umoci), [orca-build](https://github.com/cyphar/orca-build)
- [udocker](https://github.com/indigo-dc/udocker)


## Roadmap / TODO

- add kill command
- clean up CLI
- change project name
- setup CI/CD
- **0.7 beta release**
- system.Context aware processes, unpacking/packing images
- improved docker CLI compatibility regarding `build` and `run` commands in order to use cntnr to substitute docker easily in common build operations
- improved multi-user support (store per user group, file permissions, lock location)
- CLI integration tests
- rootless networking
- separate OCI CNI network hook binary
- support starting a rootless container with a user other than 0 (using proot)
- health check
- improved Docker Compose support
- service discovery integration (hook / DNS; consul, etcd)
- daemon mode
- systemd integration (cgroup, startup notification)
- **1.0 release**
- advanced logging
- support additional read-only image stores
- _maybe optional privileged network manager daemon with ACL to be used by unprivileged users to configure their container networks_


## Experiments

[Experiments with nested containers](experiments.md)
