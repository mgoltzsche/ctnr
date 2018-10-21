# Experiments with nested containers

... on an ubuntu 16.04 host


## Run ctnr container inside privileged docker container
```
docker run -ti --rm --privileged \
	-v $(pwd)/dist/bin/ctnr:/bin/ctnr \
	alpine:3.7
> ctnr run -t --network=host docker://alpine:3.7
```


## Run ctnr container inside unprivileged user's privileged ctnr container
```
dist/bin/ctnr run -t --privileged \
	-v $(pwd)/dist/bin/ctnr:/bin/ctnr \
	docker://alpine:3.7
> ctnr run -t --rootless --network=host docker://alpine:3.7
```


## Not working: Run ctnr container inside unprivileged docker container
```
docker run -ti --rm \
	-v $(pwd)/dist/bin/ctnr:/bin/ctnr \
	alpine:3.7
> ctnr run  -ti --rootless --network=host docker://alpine:3.7
```
Error: Cannot change the process namespace ("running exec setns process for init caused \"exit status 34\"")
=> seccomp denies setns

Adding a custom seccomp profile solves this problem but...
(TODO: use docker-default apparmor profile without `deny mount`, see https://github.com/moby/moby/blob/master/profiles/apparmor/template.go)
```
docker run -ti --rm --user=`id -u`:`id -g` \
	--security-opt apparmor=unconfined \
	--security-opt seccomp="$(pwd)/seccomp-container.json" \
	-v /sys/fs/cgroup:/sys/fs/cgroup:ro \
	-v "$HOME/.ctnr:/.ctnr" \
	-v "$(pwd)/dist/bin:/usr/local/bin" \
	debian:9 /bin/bash
$ ctnr --state-dir /tmp/ctnr run --verbose -ti -b test --update --rootless --no-new-keyring --no-pivot docker://alpine:3.8
```
Error: run process: container_linux.go:348: starting container process caused "process_linux.go:402: container init caused \"rootfs_linux.go:58: mounting \\\"proc\\\" to rootfs \\\"/.ctnr/bundles/test/rootfs\\\" at \\\"/proc\\\" caused \\\"operation not permitted\\\"\""
=> proc cannot be mounted
=> See https://github.com/opencontainers/runc/issues/1658


## How to analyze container problems
- Run parent container with `CAP_SYS_PTRACE` capability and child container with
  `strace -ff` to debug system calls
- Run moby's `check-config` script _(requires kernel config to be mounted)_:  
```
apk update && apk add bash
wget -O /bin/chcfg https://raw.githubusercontent.com/moby/moby/master/contrib/check-config.sh
chmod +x /bin/chcfg && chcfg
```


## Known errors and workarounds to run a container in another container

_Workarounds you do not want to do_  
_(also see https://github.com/opencontainers/runc/issues/1456)_  

- "running exec setns process for init caused \"exit status 34\""  
  -> inner container: add `--rootless` option (if that has no effect: add setns syscall to list of SCMP_ACT_ALLOW calls (TODO: which syscall exactly?))  
  -> {root} (outer container: add `--seccomp=unconfined` option)  
  -> add `--cap-add=SYS_ADMIN` to rootless outer container and `--rootless` to inner
- "mkdir /sys/fs/cgroup/cpuset/05dh[...]: permission denied"  
  -> inner container: add --rootless option  
  -> {ctnr} outer container: add --mount-cgroup=rw option
- "could not create session key: operation not permitted"  
  -> inner container: enable --no-new-keyring option  
  -> outer container: allow corresponding syscall in seccomp profile (dirty: set --seccomp=unconfined)
- "pivot_root operation not permitted"  
  -> inner container: enable --no-pivot option  
  -> outer container: seccomp: add "pivot_root" syscall to the list of SCMP_ACT_ALLOW calls

*Note regarding cgroups*:
The cgroup hierarchy can be mounted into a container using `--mount-cgroups=rw`.
Currently this is a security vulnerability since all cgroups are mounted writeable.
When using kernel >=4.6 it is possible to only make the process' cgroups writeable
(see https://github.com/opencontainers/runc/issues/225).


## Summary so far
Containers can be run in privileged containers but nesting them in unprivileged containers is still problematic.
Docker's sane seccomp and apparmor default profiles deny syscalls that are required to run a container.
The seccomp profile denies `setns` and a few other syscalls. The apparmor profile denies `mount`.
Unfortunately it still doesn't run when apparmor is disabled (or better a custom profile provided that allows mount)
and a custom seccomp profile is provided since /proc cannot be mounted since masked by docker
(see https://github.com/opencontainers/runc/issues/1658,
https://lists.linuxfoundation.org/pipermail/containers/2018-April/038864.html
and https://www.mail-archive.com/linux-kernel@vger.kernel.org/msg1533642.html).