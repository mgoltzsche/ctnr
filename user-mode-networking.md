# User-mode networking

This document describes how to achieve networking in unprivileged containers
using [CNI](https://github.com/containernetworking/cni) plugins.  
It requires the [slirp-cni-plugin](https://github.com/mgoltzsche/slirp-cni-plugin) in the `CNI_PATH`
and [slirp4netns](https://github.com/rootless-containers/slirp4netns) in the `PATH`.

## Enable user-mode networking ("slirp")

Using the [slirp-cni-plugin](https://github.com/mgoltzsche/slirp-cni-plugin)
user-mode networking can be enabled. The plugin uses
[slirp4netns](https://github.com/rootless-containers/slirp4netns)
to emulate the TCP/IP stack within the user namespace.  

ctnr comes with a built-in `slirp` network configuration that looks as follows:
```
{
    "name": "slirp",
    "type": "slirp",
	"mtu": 1500
}
```
_(The configuration can be customized by writing a corresponding CNI `*.conf`/`*.json` file
into the `~/.cni/net.d` directory (or `NETCONFPATH` env var value))_  

It can be used with ctnr for instance by running the following within the repository directory:
```
$ dist/bin/ctnr run -ti -b test --update --privileged \
	-v "$(pwd)/dist:/usr/local" \
	-v "$(pwd)/image-policy-example.json:/etc/containers/policy.json" \
	--network=slirp \
	--image-policy=image-policy-example.json \
	docker://alpine:3.8
/ # ip addr
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN qlen 1
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
    inet6 ::1/128 scope host 
       valid_lft forever preferred_lft forever
2: cni0: <BROADCAST,UP,LOWER_UP> mtu 1500 qdisc pfifo_fast state UNKNOWN qlen 1000
    link/ether 26:89:92:fc:b4:4e brd ff:ff:ff:ff:ff:ff
    inet 10.0.2.100/24 brd 10.0.2.255 scope global cni0
       valid_lft forever preferred_lft forever
    inet6 fe80::2489:92ff:fefc:b44e/64 scope link 
       valid_lft forever preferred_lft forever
/ # wget --spider http://example.org
Connecting to example.org (93.184.216.34:80)
```
Please note that from inside the container the host network as well as
the internet can be reached but not the other way around.
A container with a slirp network always has the fixed IP `10.0.2.100`
and cannot reach any other container on the host.

## Enable communication between containers

In many cases you may want multiple containers to communicate with each other.
This can be done efficiently within a user namespace where you can use other CNI plugins like the
[bridge plugin](https://github.com/containernetworking/plugins/tree/master/plugins/main/bridge).  

With ctnr you can create a privileged outer container with a `slirp` network
and nest unprivileged containers that should be able to communicate with each
other within that container.
The nested containers require the `bridge` network in addition to the `slirp` network.
For each child container the bridge plugin creates both a veth within the
container and one in the outer container connecting each child container
with the outer container's namespace.  

ctnr comes with a built-in `bridge` network configuration that looks as follows:
```
{
	"type": "bridge",
	"name": "bridge",
	"bridge": "ctnr-bridge",
	"ipMasq": true,
	"isGateway": true,
	"ipam": {
		"type": "host-local",
		"subnet": "10.2.0.0/24",
		"routes": [{
				"dst": "0.0.0.0/0"
		}]
	},
	"dns": {
		"nameservers": [ "10.2.0.1" ]
	}
}
```

To see it in action run the following example within the repository directory.  

Terminal 1: Create the outer and a nested container:
```
$ dist/bin/ctnr run -ti -b test --update --privileged \
	-v "$(pwd)/dist:/usr/local" \
	-v "$(pwd)/image-policy-example.json:/etc/containers/policy.json" \
	-v "$HOME/.ctnr:/root/.ctnr" \
	--network=slirp \
	--image-policy=image-policy-example.json \
	docker://alpine:3.8
/ # ctnr run -ti --rootless --network=slirp,bridge docker://alpine:3.8
/ # ip -4 route get 10.2.0.0 | awk {'print $6'}
10.2.0.2
/ # nc -lk -p 8080 -e echo hello
```
Terminal 2: Create a second nested container and communicate with the first nested container:
```
$ ctnr exec test ctnr run --rootless --network=slirp,bridge docker://alpine:3.8 nc 10.2.0.2 8080
hello
```

