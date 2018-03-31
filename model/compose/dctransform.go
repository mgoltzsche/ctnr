package compose

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	//	"runtime"
	"strings"

	"github.com/docker/cli/cli/compose/loader"
	"github.com/docker/cli/cli/compose/types"
	//"github.com/hashicorp/go-multierror"
	"github.com/mgoltzsche/cntnr/model"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/pkg/sliceutils"
	"github.com/pkg/errors"
)

//
// Currently only Docker Compose 3 schema is supported.
// Let's hope soon the older schema versions are supported as well
// when this is merged: https://github.com/docker/cli/pull/573
// and containers/image updated their github.com/docker/docker dependency
//

func Load(file, cwd string, env map[string]string, warn log.Logger) (r *model.CompoundServices, err error) {
	defer exterrors.Wrapd(&err, "load docker compose file")
	absCwd := cwd
	if absCwd, err = filepath.Abs(cwd); err != nil {
		return
	}
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return
	}
	dcyml, err := loader.ParseYAML(b)
	if err != nil {
		return
	}
	cfg, err := loader.Load(types.ConfigDetails{
		WorkingDir:  cwd,
		ConfigFiles: []types.ConfigFile{types.ConfigFile{file, dcyml}},
		Environment: env,
	})
	if err != nil {
		return
	}
	return transform(cfg, absCwd, warn)
}

func GetEnv() map[string]string {
	r := map[string]string{}
	for _, entry := range os.Environ() {
		s := strings.SplitN(entry, "=", 2)
		if len(s) == 2 {
			r[s[0]] = s[1]
		} else {
			r[s[0]] = ""
		}
	}
	return r
}

func transform(cfg *types.Config, cwd string, warn log.Logger) (r *model.CompoundServices, err error) {
	services, err := toServices(cfg.Services)
	if err != nil {
		return
	}
	r = &model.CompoundServices{
		Dir:      cwd,
		Volumes:  toVolumes(cfg.Volumes, warn),
		Services: services,
		// TODO: map networks, secrets
	}
	return
}

func toVolumes(vols map[string]types.VolumeConfig, warn log.Logger) map[string]model.Volume {
	r := map[string]model.Volume{}
	for name, vol := range vols {
		v := model.Volume{}
		if vol.External.External {
			v.External = vol.Name
			if vol.External.Name != "" {
				v.External = vol.External.Name
			}
		} else {
			warn.Printf("adding unsupported volume %v as temporary volume", vol)
		}
		r[name] = v
	}
	return r
}

func toServices(services []types.ServiceConfig) (r map[string]model.Service, err error) {
	r = map[string]model.Service{}
	for _, service := range services {
		if r[service.Name], err = toService(service); err != nil {
			return
		}
	}
	return
}

func toService(s types.ServiceConfig) (r model.Service, err error) {
	r = model.NewService(s.Name)
	r.Build = toBuild(s.Build)
	r.CapAdd = s.CapAdd
	r.CapDrop = s.CapDrop
	// s.CgroupParent
	r.Command = []string(s.Command)
	// TODO:
	// DependsOn
	// CredentialSpec
	// Deploy
	// Devices
	r.Dns = []string(s.DNS)
	r.DnsSearch = []string(s.DNSSearch)
	r.Domainname = s.DomainName
	r.Entrypoint = []string(s.Entrypoint)
	r.Environment = toStringMap(s.Environment)
	// EnvFile
	r.Expose = []string(s.Expose)
	// ExternalLinks
	if r.ExtraHosts, err = toExtraHosts(s.ExtraHosts); err != nil {
		return
	}
	r.Hostname = s.ContainerName
	// Healthcheck
	r.Image = "docker://" + s.Image
	// Ipc
	// Labels
	// Links
	// Logging
	// MacAddress
	// NetworkMode
	// Pid
	if r.Ports, err = toPorts(s.Ports); err != nil {
		return
	}
	// Privileged
	r.ReadOnly = s.ReadOnly
	// Restart
	// Secrets
	// SecurityOpt
	r.StdinOpen = s.StdinOpen
	r.StopGracePeriod = s.StopGracePeriod
	r.StopSignal = s.StopSignal
	// Tmpfs
	r.Tty = s.Tty
	// Ulimits
	r.User = toUser(s.User)
	r.Volumes = toVolumeMounts(s.Volumes)
	r.Cwd = s.WorkingDir
	// Isolation
	return
}

func toBuild(s types.BuildConfig) (r *model.ImageBuild) {
	if s.Context != "" || s.Dockerfile != "" {
		r = &model.ImageBuild{
			Context:    s.Context,
			Dockerfile: s.Dockerfile,
			Args:       toStringMap(s.Args),
		}
	}
	return
}

func toStringMap(m types.MappingWithEquals) map[string]string {
	r := map[string]string{}
	for k, v := range (map[string]*string)(m) {
		if v == nil {
			r[k] = ""
		} else {
			r[k] = *v
		}
	}
	return r
}

func toExtraHosts(hl types.HostsList) ([]model.ExtraHost, error) {
	l := []string(hl)
	r := make([]model.ExtraHost, 0, len(l))
	for _, h := range hl {
		he := strings.SplitN(h, ":", 2)
		if len(he) != 2 {
			return nil, errors.Errorf("invalid extra_hosts entry: expected format host:ip but was %q", h)
		}
		r = append(r, model.ExtraHost{
			Name: he[0],
			Ip:   he[1],
		})
	}
	return r, nil
}

func toPorts(ports []types.ServicePortConfig) (r []model.PortBinding, err error) {
	r = make([]model.PortBinding, 0, len(ports))
	for _, p := range ports {
		if p.Target > 65535 {
			return nil, errors.Errorf("invalid target port %d exceeded range", p.Target)
		}
		if p.Published > 65535 {
			return nil, errors.Errorf("invalid published port %d exceeded range", p.Published)
		}
		r = append(r, model.PortBinding{
			Target:    uint16(p.Target),
			Published: uint16(p.Published),
			Protocol:  p.Protocol,
		})
		// TODO: checkout p.Mode
	}
	return
}

func toUser(s string) (u *model.User) {
	if s == "" {
		return nil
	}
	ug := strings.SplitN(s, ":", 2)
	if len(ug) == 2 {
		u = &model.User{ug[0], ug[1], nil}
	} else {
		u = &model.User{ug[0], ug[0], nil}
	}
	return
}

func toVolumeMounts(vols []types.ServiceVolumeConfig) []model.VolumeMount {
	r := []model.VolumeMount{}
	for _, vol := range vols {
		var opts []string
		if vol.Bind != nil && vol.Bind.Propagation != "" {
			opts = strings.Split(vol.Bind.Propagation, ":")
		}
		if vol.ReadOnly {
			sliceutils.AddToSet(&opts, "ro")
		}
		if vol.Tmpfs != nil {
			opts = append(opts, fmt.Sprintf("size=%d", vol.Tmpfs.Size))
		}
		// TODO: Consistency
		r = append(r, model.VolumeMount{
			Type:    model.MountType(vol.Type), // 'volume', 'bind' or 'tmpfs'
			Source:  vol.Source,
			Target:  vol.Target,
			Options: opts,
		})
	}
	return r
}
