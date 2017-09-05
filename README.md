# cntnr

CNTNR DEVELOPMENT IS IN A VERY EARLY STATE!

cntnr is a lightweight container engine that provides a CLI on top of [runc](https://github.com/opencontainers/runc) to also manage images and build OCI runtime bundles. It is a platform to try out new runc features.
cntnr aims to ease container creation and execution as unprivileged user as well as the integration with other tools such as [systemd](https://www.freedesktop.org/wiki/Software/systemd/) and [consul](https://www.consul.io/).

Multiple containers can be composed and run in one CLI command.
The [docker compose](https://docs.docker.com/compose/compose-file/) file format is also supported.
Images can be fetched and converted from various sources using [image](https://github.com/containers/image).
Container networks are managed in OCI hooks using [CNI](https://github.com/containernetworking/cni) and its [plugins](https://github.com/containernetworking/plugins).


## Rootless containers

The ability to run a container as unprivileged user has some advantages:

- Container images can be built inside an unprivileged container (WIP, see below).
- A container can be run in a restrictive, rootless environment.
- A higher degree and more flexible level of security since you can rely on your host OS' ACL.

See [Aleksa Sarai's blog post](https://www.cyphar.com/blog/post/rootless-containers-with-runc) for more information.


### Limitations & challenges

Unfortunately container execution as unprivileged user is limited:


**A separate container network namespace cannot be configured.**
A daemon run as root could configure a separate container namespace for an unprivileged user.
In a restrictive environment without root access the host network namespace must be sufficient.


**Inside the container a process' or file's user cannot be changed.**
This is caused by the fact that all operations in the container are still run by the host user whose ID is simply mapped to 0/root inside the container.
Unfortunately this stops many official docker images as well as package managers from working. The latter is crucial to build container images.
A solution approach is to hook the corresponding system calls and simulate them without actually doing them.
To my knowlegde currently there are no complete but partial solutions as [remainroot](https://github.com/cyphar/remainroot).
The problem remains that not everything can be simulated since applications ask for the actual state and expect their earlier system calls to be applied.



## Roadmap

- separate OCI hook binary
- CLI improvements: image build, bundle run, compose create
- image/bundle store locks
- container, bundle and image garbage collection
- apply CLI/compose network configuration
- health check
- systemd integration (notify when startup complete)
- service discovery integration (consul, etcd)
- network manager daemon with ACL to be used by unprivileged users to configure their container networks
- container annotation driven env var sync with distributed KV store (consul, etcd) to e.g. auto-configure webserver/loadbalancer or for basic master election
