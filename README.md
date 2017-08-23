# cntnr

CNTNR DEVELOPMENT IS STILL IN A VERY EARLY STATE!

cntnr is a lightweight container engine based on runc/libcontainer and written in [go](https://golang.org/).
It supports high level service composition also using the [docker compose](https://docs.docker.com/compose/compose-file/) file format and creates [OCI compliant](https://github.com/opencontainers/runtime-spec) containers.
Container networks are managed in OCI hooks using [CNI](https://github.com/containernetworking/cni) and its [plugins](https://github.com/containernetworking/plugins).

The ability to run a container as unprivileged user has many advantages:

- A container can be run in a restrictive, rootless environment.
- A container can be run within an unprivileged container which can be useful when building images inside a container.
- Graphical applications like [Firefox](https://www.mozilla.org/en-US/firefox/) can be run in a container.
- It provides both a higher degree and more flexible level of security since you can rely on your host OS' ACL.

cntnr also aims to integrate with other tools as [systemd](https://www.freedesktop.org/wiki/Software/systemd/) and [consul](https://www.consul.io/).



## Roadmap

- CLI to support management of images, single containers and container compositions
- apply docker compose network configuration
- port binding (using CNI)
- direct integration of libcontainer instead of runc CLI (optional, build tag based) to ease installation and enforce compatibility
- network manager daemon with ACL to be used by unprivileged users to configure their container networks
- systemd integration (notify when startup complete)
- efficient image tree store to save disk space
- container and image garbage collection
- rootless image build without docker
- health check
- service discovery integration (consul, etcd)
- container annotation driven env var sync with distributed KV store (consul, etcd) to e.g. auto-configure webserver/loadbalancer or for basic master election
