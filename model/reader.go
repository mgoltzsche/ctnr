package model

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	shellwords "github.com/mattn/go-shellwords"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/pkg/sliceutils"
	"gopkg.in/yaml.v2"
)

func LoadProject(file string, warn log.Logger) (r *Project, err error) {
	file, err = filepath.Abs(file)
	if err != nil {
		return
	}
	r = &Project{Dir: filepath.Dir(file)}
	env, err := readEnvironment()
	if err != nil {
		return
	}
	sub := NewSubstitution(env, warn)
	err = loadFromComposeYAML(file, sub, r)
	return
}

func loadFromJSON(file string, r *Project) error {
	b, err := ioutil.ReadFile(filepath.FromSlash(file))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, r)
}

func loadFromComposeYAML(file string, sub Substitution, r *Project) error {
	c, err := readComposeYAML(file)
	if err != nil {
		return err
	}
	return convertCompose(c, sub, r)
}

func readComposeYAML(file string) (*dockerCompose, error) {
	b, err := ioutil.ReadFile(filepath.FromSlash(file))
	if err != nil {
		return nil, fmt.Errorf("Cannot read compose file: %v", err)
	}
	dc := &dockerCompose{}
	err = yaml.Unmarshal(b, dc)
	dc.Dir = filepath.Dir(file)
	return dc, err
}

func convertCompose(c *dockerCompose, sub Substitution, r *Project) error {
	if c.Services == nil || len(c.Services) == 0 {
		return fmt.Errorf("No services defined in %s", c.Dir)
	}
	toVolumes(c, sub, &r.Volumes, ".volumes")
	r.Services = map[string]Service{}
	for k, v := range c.Services {
		s := NewService(k)
		envFileEnv := map[string]string{}
		err := convertComposeService(c, v, sub, r, s, envFileEnv)
		if err != nil {
			return err
		}

		// Apply environment from env files if not yet set (defaults)
		for k, v := range envFileEnv {
			if _, ok := s.Environment[k]; !ok {
				s.Environment[k] = v
			}
		}

		r.Services[k] = *s
	}
	return nil
}

func toVolumes(c *dockerCompose, sub Substitution, rp *map[string]Volume, path string) error {
	r := map[string]Volume{}
	for name, info := range c.Volumes {
		name = sub(name)
		externalName := ""
		switch t := info.(type) {
		case nil:
		case map[interface{}]interface{}:
			ext := info.(map[interface{}]interface{})["external"]
			switch ext.(type) {
			case map[interface{}]interface{}:
				externalName = toString(ext.(map[interface{}]interface{})["name"], sub, path+"."+name+".external.name")
			default:
				isext, err := toBool(ext, sub, path+"."+name+".external")
				if err != nil {
					return err
				}
				if isext {
					externalName = name
				}
			}
		default:
			return fmt.Errorf("Unsupported entry type %v at %s", t, path+".name")
		}
		r[name] = Volume{"", externalName}
	}
	*rp = r
	return nil
}

func convertComposeService(c *dockerCompose, s *dcService, sub Substitution, p *Project, d *Service, envFileEnv map[string]string) (err error) {
	l := "service." + d.Name

	// Extend service (convert recursively)
	if s.Extends != nil {
		var yml *dockerCompose
		if s.Extends.File == "" {
			yml = c
		} else {
			yml, err = readComposeYAML(absFile(s.Extends.File, c.Dir))
			if err != nil {
				return fmt.Errorf("services.%s.extends.file: %v", d.Name, err)
			}
		}
		base := yml.Services[s.Extends.Service]
		if base == nil {
			return fmt.Errorf("services.%s.extends.service: Invalid reference", d.Name)
		}
		err = convertComposeService(yml, base, sub, p, d, envFileEnv)
		if err != nil {
			return fmt.Errorf("Failed to read base service %q in %s: %v", d.Name, yml.Dir, err)
		}
	}

	// Convert properties
	if s.Image != "" {
		if s.Build == nil {
			d.Image = "docker://" + sub(s.Image)
		} else {
			d.Image = "docker-daemon://" + sub(s.Image)
		}
	}

	if err = toImageBuild(s.Build, sub, &d.Build, c.Dir, p.Dir, l+".build"); err != nil {
		return
	}

	if s.Entrypoint != nil {
		d.Entrypoint, err = toStringArray(s.Entrypoint, sub, []string{}, l+".entrypoint")
		if err != nil {
			return
		}
	}
	if s.Command != nil {
		d.Command, err = toStringArray(s.Command, sub, []string{}, l+".command")
		if err != nil {
			return
		}
	}
	if s.WorkingDir != "" {
		d.Cwd = toString(s.WorkingDir, sub, l+".working_dir")
	}
	if s.CapAdd != nil {
		d.CapAdd = append(d.CapAdd, s.CapAdd...)
	}
	if s.CapDrop != nil {
		for _, dropCap := range d.CapDrop {
			sliceutils.RemoveFromSet(&d.CapAdd, dropCap)
		}
		d.CapDrop = append(d.CapDrop, s.CapDrop...)
	}
	if s.ReadOnly != "" {
		d.ReadOnly, err = toBool(s.ReadOnly, sub, l+".read_only")
		if err != nil {
			return
		}
	}
	if s.StdinOpen != "" {
		d.StdinOpen, err = toBool(s.StdinOpen, sub, l+".read_only")
		if err != nil {
			return
		}
	}
	if s.Tty != "" {
		d.Tty, err = toBool(s.Tty, sub, l+".tty")
		if err != nil {
			return
		}
	}
	if s.EnvFile != nil {
		for _, f := range s.EnvFile {
			err = applyEnvFile(absFile(f, c.Dir), envFileEnv)
			if err != nil {
				return
			}
		}
	}
	d.Environment, err = toStringMap(s.Environment, sub, d.Environment, l+".environment")
	if err != nil {
		return
	}
	if s.Hostname != "" {
		d.Hostname = sub(s.Hostname)
	}
	if s.Domainname != "" {
		d.Domainname = sub(s.Domainname)
	}
	if s.Dns != nil {
		d.Dns, err = toStringArray(s.Dns, sub, d.Dns, l+".dns")
		if err != nil {
			return
		}
	}
	if s.DnsSearch != nil {
		d.DnsSearch, err = toStringArray(s.DnsSearch, sub, d.DnsSearch, l+".dns_search")
		if err != nil {
			return
		}
	}
	if err = toExtraHosts(s.ExtraHosts, sub, &d.ExtraHosts, l+".extra_hosts"); err != nil {
		return
	}
	toExpose(s.Expose, sub, &d.Expose, l+".expose")
	if err = toPorts(s.Ports, sub, &d.Ports, l+".ports"); err != nil {
		return
	}
	if s.StopSignal != "" {
		d.StopSignal = sub(s.StopSignal)
	}
	if s.StopGracePeriod != "" {
		d.StopGracePeriod, err = toDuration(s.StopGracePeriod, "10s", sub, l+".stop_grace_period")
		if err != nil {
			return
		}
	}
	if err = toVolumeMounts(s.Volumes, sub, c.Dir, p.Dir, &d.Volumes, l+".volumes"); err != nil {
		return
	}
	if d.HealthCheck != nil {
		d.HealthCheck, err = toHealthCheckDescriptor(s.HealthCheck, sub, l+".healthcheck")
		if err != nil {
			return err
		}
	}
	return
}

func toExtraHosts(h []string, sub Substitution, rp *[]ExtraHost, path string) error {
	r := *rp
	if r == nil {
		r = []ExtraHost{}
	}
	for _, l := range h {
		s := strings.SplitN(l, ":", 2)
		if len(s) != 2 {
			return fmt.Errorf("Invalid entry at %s: %s. Expected format: HOST:IP", path, l)
		}
		host := sub(s[0])
		ip := sub(s[1])
		if host == "" || ip == "" {
			return fmt.Errorf("Empty host or IP at %s: %s", path, l)
		}
		r = append(r, ExtraHost{host, ip})
	}
	*rp = r
	return nil
}

func toExpose(p []string, sub Substitution, rp *[]string, path string) {
	r := *rp
	if r == nil {
		r = []string{}
	}
	m := map[string]bool{}
	for _, e := range p {
		e = sub(e)
		if ok := m[e]; !ok {
			m[e] = true
			r = append(r, e)
		}
	}
	*rp = r
}

func toPorts(p []string, sub Substitution, rp *[]PortBinding, path string) error {
	if p != nil {
		for _, e := range p {
			// TODO: also support long syntax
			e = sub(e)
			if err := ParsePortBinding(e, rp); err != nil {
				return fmt.Errorf("%s: %s", path, err)
			}
		}
	}
	return nil
}

func ParsePortBinding(expr string, r *[]PortBinding) error {
	sp := strings.Split(expr, "/")
	if len(sp) > 2 {
		return fmt.Errorf("Invalid port entry %q", expr)
	}
	prot := "tcp"
	if len(sp) == 2 {
		prot = strings.ToLower(sp[1])
	}

	var hostIp, hostPortExpr, targetPortExpr string
	psi := strings.LastIndex(sp[0], ":")
	hostPart := sp[0]
	if psi > 0 && psi+1 < len(sp[0]) {
		hostPart = sp[0][:psi]
		targetPortExpr = sp[0][psi+1:]
	}
	isi := strings.LastIndex(hostPart, ":")
	hostPortExpr = hostPart
	if isi > 0 && isi+1 > len(hostPart) {
		hostIp = hostPart[:isi]
		hostPortExpr = hostPart[isi+1:]
	}
	if targetPortExpr == "" {
		targetPortExpr = hostPortExpr
	}
	hostFrom, hostTo, err := toPortRange(hostPortExpr)
	if err != nil {
		return err
	}
	targetFrom, targetTo, err := toPortRange(targetPortExpr)
	if err != nil {
		return err
	}
	rangeSize := targetTo - targetFrom
	if (hostTo - hostFrom) != rangeSize {
		return fmt.Errorf("Port %q's range size differs between host and destination", expr)
	}
	for i := 0; i <= rangeSize; i++ {
		targetPort := targetFrom + i
		pubPort := hostFrom + i
		if targetPort < 0 || targetPort > 65535 {
			return fmt.Errorf("Target port %d exceeded range", targetPort)
		}
		if pubPort < 0 || pubPort > 65535 {
			return fmt.Errorf("Published port %d exceeded range", pubPort)
		}
		b := PortBinding{uint16(targetPort), uint16(pubPort), prot, hostIp}
		if *r == nil {
			*r = []PortBinding{b}
		} else {
			*r = append(*r, b)
		}
	}
	return nil
}

func toPortRange(rangeExpr string) (from, to int, err error) {
	s := strings.Split(rangeExpr, "-")
	if len(s) < 3 {
		from, err = strconv.Atoi(s[0])
		if err == nil {
			if len(s) == 2 {
				to, err = strconv.Atoi(s[1])
				if err == nil && from <= to {
					return
				}
			} else {
				to = from
				return
			}
		}
	}
	err = fmt.Errorf("Invalid port range %q", rangeExpr)
	return
}

func toVolumeMounts(dcVols []interface{}, sub Substitution, baseFile, destBaseFile string, rp *[]VolumeMount, path string) (err error) {
	r := *rp
	if r == nil {
		r = []VolumeMount{}
	}
	// TODO: maybe remove overwritten volumes
	for _, e := range dcVols {
		var v VolumeMount

		switch t := e.(type) {
		case string:
			if err = ParseVolumeMount(sub(e.(string)), &v); err != nil {
				return fmt.Errorf("%s: %s", path, err)
			}
		case map[interface{}]interface{}:
			m := e.(map[interface{}]interface{})
			vtype := toString(m["type"], sub, path+".type")
			v.Source = toString(m["source"], sub, path+".source")
			v.Target = toString(m["target"], sub, path+".target")
			v.Options = []string{}
			if vtype == "" {
				vtype = "volume"
			}
			optMap, err := toStringMap(m[vtype], sub, map[string]string{}, path+"."+vtype)
			if err != nil {
				return err
			}
			for k, p := range optMap {
				if p == "" {
					v.Options = append(v.Options, k)
				} else {
					v.Options = append(v.Options, k+"="+p)
				}
			}
			if v.Target == "" {
				return fmt.Errorf("No volume mount target specified at %s: %v", path, e)
			}
		default:
			return fmt.Errorf("Unsupported element type %v at %s", t, path)
		}

		if v.Source != "" && !v.IsNamedVolume() {
			v.Source = translatePath(v.Source, baseFile, destBaseFile)
		}

		r = append(r, v)
	}
	*rp = r
	return nil
}

func ParseVolumeMount(expr string, r *VolumeMount) (err error) {
	r.Options = []string{}
	s := strings.Split(expr, ":")
	switch len(s) {
	case 1:
		r.Source = ""
		r.Target = s[0]
	default:
		r.Source = s[0]
		r.Target = s[1]
		r.Options = s[2:]
	}
	if r.Target == "" {
		err = fmt.Errorf("No volume mount target specified: %s", expr)
	}
	return
}

func toImageBuild(s interface{}, sub Substitution, rp **ImageBuild, baseFile, destBaseFile, path string) (err error) {
	switch s.(type) {
	case string:
		ctx := translatePath(sub(s.(string)), baseFile, destBaseFile)
		*rp = &ImageBuild{ctx, "", nil}
	case map[interface{}]interface{}:
		m := s.(map[interface{}]interface{})
		r := *rp
		if r == nil {
			r = &ImageBuild{}
		}
		r.Args = map[string]string{}
		for k, v := range m {
			ks := toString(k, sub, path)
			pk := path + "." + ks
			switch ks {
			case "context":
				r.Context = translatePath(toString(v, sub, pk), baseFile, destBaseFile)
			case "dockerfile":
				r.Dockerfile = toString(v, sub, pk)
			case "args":
				r.Args, err = toStringMap(v, sub, r.Args, pk)
				if err != nil {
					return
				}
			}
		}
		*rp = r
	case nil:
	default:
		err = fmt.Errorf("string or []string expected at %s but was: %s", path, s)
	}
	return
}

func toHealthCheckDescriptor(c *dcHealthCheck, sub Substitution, path string) (*Check, error) {
	if c == nil {
		return nil, nil
	} else {
		test, err := toStringArray(c.Test, sub, []string{}, path)
		if err != nil {
			return nil, err
		}
		if len(test) == 0 {
			return nil, fmt.Errorf("%s: undefined health test command", path+".test")
		}
		var cmd []string
		switch test[0] {
		case "CMD":
			cmd = test[1:]
		case "CMD-SHELL":
			cmd = append([]string{"/bin/sh", "-c"}, test[1:]...)
		default:
			cmd = append([]string{"/bin/sh", "-c"}, strings.Join(test, " "))
		}
		interval, err := toDuration(c.Interval, "30s", sub, path+".interval")
		if err != nil {
			return nil, err
		}
		timeout, err := toDuration(c.Timeout, "10s", sub, path+".timeout")
		if err != nil {
			return nil, err
		}
		disable, err := toBool(c.Disable, sub, path+".disable")
		if err != nil {
			return nil, err
		}
		retriesStr := toString(c.Retries, sub, path+".retries")
		retries, err := strconv.ParseUint(retriesStr, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%s: %s", path, err)
		}
		return &Check{cmd, interval, timeout, uint(retries), disable}, nil
	}
}

func toStringArray(v interface{}, sub Substitution, r []string, path string) ([]string, error) {
	if r == nil {
		r = []string{}
	}
	switch v.(type) {
	case []interface{}:
		l := v.([]interface{})
		if r == nil {
			r = make([]string, 0, len(l))
		}
		for _, u := range l {
			str := toString(u, sub, path)
			r = append(r, str)
		}
	case string:
		l, err := shellwords.Parse(sub(v.(string)))
		if err != nil {
			return r, err
		}
		r = append(r, l...)
	case nil:
	default:
		return r, fmt.Errorf("string or []string expected at %s but was: %s", path, v)
	}
	return r, nil
}

func toStringMap(v interface{}, sub Substitution, r map[string]string, path string) (map[string]string, error) {
	if r == nil {
		r = map[string]string{}
	}
	switch t := v.(type) {
	case map[interface{}]interface{}:
		u := v.(map[interface{}]interface{})
		for k, v := range u {
			r[toString(k, sub, path)] = toString(v, sub, path)
		}
		return r, nil
	case []interface{}:
		for _, u := range v.([]interface{}) {
			e := toString(u, sub, path)
			s := strings.SplitN(e, "=", 2)
			if len(s) == 2 {
				r[s[0]] = s[1]
			} else {
				r[s[0]] = ""
			}
		}
		return r, nil
	case nil:
		return r, nil
	default:
		return nil, fmt.Errorf("map[string]string or []string expected at %s but was: %v", path, t)
	}
}

func toDuration(v, defaultVal string, sub Substitution, p string) (time.Duration, error) {
	v = sub(v)
	if v == "" {
		v = defaultVal
	}
	if v == "" {
		return 0, nil
	}
	d, e := time.ParseDuration(v)
	if e != nil {
		return 0, fmt.Errorf("%s: invalid duration: %q", p, v)
	}
	return d, nil
}

func toBool(v interface{}, sub Substitution, path string) (bool, error) {
	s := toString(v, sub, path)
	if s == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(sub(s))
	if err != nil {
		return b, fmt.Errorf("%s: Cannot parse %q as bool", path, s)
	}
	return b, nil
}

func toString(v interface{}, sub Substitution, path string) string {
	switch t := v.(type) {
	case string:
		return sub(v.(string))
	case bool:
		return strconv.FormatBool(v.(bool))
	case int:
		return strconv.Itoa(v.(int))
	case nil:
		return ""
	default:
		panic(fmt.Sprintf("String expected at %s but was %v", path, t))
	}
}

func readEnvironment() (map[string]string, error) {
	env := map[string]string{}
	_, err := os.Stat(".env")
	if err == nil {
		err = applyEnvFile(".env", env)
	} else if os.IsNotExist(err) {
		err = nil
	}
	for _, e := range os.Environ() {
		s := strings.SplitN(e, "=", 2)
		env[s[0]] = s[1]
	}
	return env, err
}

func applyEnvFile(file string, r map[string]string) error {
	f, err := os.Open(filepath.FromSlash(file))
	if err != nil {
		return fmt.Errorf("Cannot open env file %q: %s", file, err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	i := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && strings.Index(line, "#") != 0 {
			kv := strings.SplitN(line, "=", 2)
			if len(kv) != 2 {
				return fmt.Errorf("Invalid env file entry at %s:%d: %q", file, i, kv)
			}
			r[kv[0]] = kv[1]
		}
		i++
	}
	if err = scanner.Err(); err != nil {
		return fmt.Errorf("Cannot read env file %q: %s", file, err)
	}
	return nil
}

func absFile(p, base string) string {
	if filepath.IsAbs(p) {
		return p
	} else {
		return filepath.Join(base, p)
	}
}

func translatePath(path, base, destBase string) string {
	if filepath.IsAbs(path) {
		return path
	}
	abs := filepath.Join(base, path)
	r, err := filepath.Rel(destBase, abs)
	if err != nil {
		panic("Not an absolute directory path: " + base)
	}
	if len(path) == 0 || !filepath.IsAbs(path) && !(path == "~" || len(path) > 1 && path[0:2] == "~/") {
		r = "./" + r
	}
	return r
}

// See https://docs.docker.com/compose/compose-file/
type dockerCompose struct {
	Version  string
	Dir      string
	Services map[string]*dcService
	Volumes  map[string]interface{}
}

type dcService struct {
	Extends         *dcServiceExtension
	Image           string         `yaml:"image"`
	Build           interface{}    `yaml:"build"` // string or map[interface{}]interface{}
	Hostname        string         `yaml:"hostname"`
	Domainname      string         `yaml:"domainname"`
	Dns             interface{}    `yaml:"dns"`
	DnsSearch       interface{}    `yaml:"dns_search"`
	ExtraHosts      []string       `yaml:"extra_hosts"`
	Entrypoint      interface{}    `yaml:"entrypoint"` // string or array
	Command         interface{}    `yaml:"command"`    // string or array
	WorkingDir      string         `yaml:"working_dir"`
	CapAdd          []string       `yaml:"cap_add"`
	CapDrop         []string       `yaml:"cap_drop"`
	StdinOpen       string         `yaml:"stdin_open"`
	Tty             string         `yaml:"tty"`
	ReadOnly        string         `yaml:"read_only"`
	EnvFile         []string       `yaml:"env_file"`
	Environment     interface{}    `yaml:"environment"` // array of VAR=VAL or map
	HealthCheck     *dcHealthCheck `yaml:"healthcheck"`
	Expose          []string       `yaml:"expose"`
	Ports           []string       `yaml:"ports"`
	Volumes         []interface{}  `yaml:"volumes"`
	StopSignal      string         `yaml:"stop_signal"`
	StopGracePeriod string         `yaml:"stop_grace_period"`
	// TODO: Checkout 'secret' dc property
}

type dcServiceExtension struct {
	File    string
	Service string
}

type dcHealthCheck struct {
	Test     interface{}
	Interval string
	Timeout  string
	Retries  string
	Disable  string
}
