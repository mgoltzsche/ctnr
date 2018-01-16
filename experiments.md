# Experiments with nested containers

... on an ubuntu 16.04 host


## Run cntnr container inside privileged docker container
```
docker run -ti --privileged --name cnestedpriv --rm \
	-v $(pwd)/dist/bin/cntnr:/bin/cntnr \
	alpine:3.7
> cntnr run -t --network=host docker://alpine:3.7
```


## Run cntnr container inside unprivileged user's privileged cntnr container
```
dist/bin/cntnr run -b outerc --update -t \
	--cap-add=SYS_ADMIN \
	--mount=./dist/bin/cntnr:/bin/cntnr:exec:ro \
	--mount=./dist/cni-plugins:/cni \
	--mount=/boot/config-4.4.0-104-generic:/boot/config-4.4.0-104-generic \
	docker://alpine:3.7
> cntnr run -t --rootless --no-new-keyring --no-pivot --network=host docker://alpine:3.7
```
Attention: The parent container has low isolation to the calling user due to the
CAP_SYS_ADMIN capability and the `--no-new-keyring` option allows the inner
container to obtain secrets from the parent.  
This still may provide a sufficient level of security in some use cases when run by
an unprivileged user.


## Not working: Run cntnr container inside unprivileged docker container
```
docker run -ti --name cnested --rm \
	-v $(pwd)/dist/bin/cntnr:/bin/cntnr \
	-v /boot/config-4.4.0-104-generic:/boot/config-4.4.0-104-generic \
	alpine:3.7
> cntnr run  -ti --rootless --network=host docker://alpine:3.7
```
Error: Cannot change the process namespace ("running exec setns process for init caused \"exit status 34\"")


## How to analyze container problems
- Run parent container with `CAP_SYS_PTRACE` capability and child container with
  `strace -f` to debug system calls
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
  -> {cntnr} outer container: add --mount-cgroup=rw option
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


## Conclusion
Containers can be run in privileged but not in unprivileged containers.
The outer container requires the CAP_SYS_ADMIN capability which basically allows it to act as the calling user.  

Alternatively a seccomp profile could be prepared that allows only the syscalls
necessary to run a container - especially since CAP_SYS_ADMIN is overloaded
(see http://man7.org/linux/man-pages/man7/capabilities.7.html).  
(TODO: investigate)
