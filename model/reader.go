package model

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/mgoltzsche/cntnr/log"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func LoadProject(file, volDir string, warn log.Logger) (r *Project, err error) {
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
	err = loadFromComposeYAML(file, sub, volDir, r)
	return
}

func loadFromJSON(file string, r *Project) error {
	b, err := ioutil.ReadFile(filepath.FromSlash(file))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, r)
}

func loadFromComposeYAML(file string, sub Substitution, volDir string, r *Project) error {
	c, err := readComposeYAML(file)
	if err != nil {
		return err
	}
	return convertCompose(c, sub, volDir, r)
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

func convertCompose(c *dockerCompose, sub Substitution, volDir string, r *Project) error {
	if c.Services == nil || len(c.Services) == 0 {
		return fmt.Errorf("No services defined in %s", c.Dir)
	}
	r.Services = map[string]Service{}
	for k, v := range c.Services {
		s := NewService(k)
		envFileEnv := map[string]string{}
		err := convertComposeService(c, v, sub, volDir, r, s, envFileEnv)
		if err != nil {
			return err
		}

		// Apply environment from env files if not yet set (defaults)
		for k, v := range envFileEnv {
			if _, ok := s.Environment[k]; !ok {
				s.Environment[k] = v
			}
		}

		// Apply special HTTP_* keys
		// TODO: unify this by marking env vars as syncable with a KV store
		if httpHost := s.Environment["HTTP_HOST"]; httpHost != "" {
			httpPort := s.Environment["HTTP_PORT"]
			if httpPort == "" {
				return fmt.Errorf("HTTP_HOST without HTTP_PORT env var defined in service %q" + s.Name)
			}
			s.SharedKeys = map[string]string{}
			s.SharedKeys["http/"+httpHost] = s.Name + ":" + httpPort
		}
		r.Services[k] = *s
	}
	return nil
}

func convertComposeService(c *dockerCompose, s *dcService, sub Substitution, volDir string, p *Project, d *Service, envFileEnv map[string]string) (err error) {
	l := "service." + d.Name

	// Extend service
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
		err = convertComposeService(yml, base, sub, volDir, p, d, envFileEnv)
		if err != nil {
			return fmt.Errorf("Failed to read base service %q in %s: %v", d.Name, yml.Dir, err)
		}
	}

	// Image
	if s.Image != "" {
		if s.Build == nil {
			d.Image = "docker://" + sub(s.Image)
		} else {
			d.Image = "docker-daemon://" + sub(s.Image)
		}
	}

	d.Build, err = toImageBuild(s.Build, sub, d.Build, c.Dir, p.Dir, l+".build")
	if err != nil {
		return
	}

	if s.Entrypoint != nil {
		d.Entrypoint, err = toStringArray(s.Entrypoint, sub, l+".entrypoint")
		if err != nil {
			return
		}
	}
	if s.Command != nil {
		d.Command, err = toStringArray(s.Command, sub, l+".command")
		if err != nil {
			return
		}
	}
	if s.WorkingDir != "" {
		d.Cwd = toString(s.WorkingDir, sub, l+".working_dir")
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
	d.Expose = toExpose(s.Expose, sub, d.Expose, l+".expose")
	d.Ports, err = toPorts(s.Ports, sub, d.Ports, l+".ports")
	if err != nil {
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
	d.Mounts, err = toVolumeMounts(s.Volumes, sub, volDir, c.Dir, p.Dir, d.Mounts, l+".volumes")
	if err != nil {
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

func toExpose(p []string, sub Substitution, r []string, path string) []string {
	if r == nil {
		r = []string{}
	}
	if p == nil {
		return r
	}
	m := map[string]bool{}
	for _, e := range p {
		e = sub(e)
		if ok := m[e]; !ok {
			m[e] = true
			r = append(r, e)
		}
	}
	return r
}

func toPorts(p []string, sub Substitution, r []PortBinding, path string) ([]PortBinding, error) {
	if r == nil {
		r = []PortBinding{}
	}
	if p == nil {
		return r, nil
	}
	for _, e := range p {
		e = sub(e)
		sp := strings.Split(e, "/")
		if len(sp) > 2 {
			return r, fmt.Errorf("Invalid port entry %q at %s", e, path)
		}
		prot := "tcp"
		if len(sp) == 2 {
			prot = strings.ToLower(sp[1])
		}
		s := strings.Split(sp[0], ":")
		if len(s) > 2 {
			return r, fmt.Errorf("Invalid port entry %q at %s", e, path)
		}
		var hostPortExpr, targetPortExpr string
		switch len(s) {
		case 1:
			hostPortExpr = s[0]
			targetPortExpr = hostPortExpr
		case 2:
			hostPortExpr = s[0]
			targetPortExpr = s[1]
		}
		hostFrom, hostTo, err := toPortRange(hostPortExpr, path)
		if err != nil {
			return r, err
		}
		targetFrom, targetTo, err := toPortRange(targetPortExpr, path)
		if err != nil {
			return r, err
		}
		rangeSize := targetTo - targetFrom
		if (hostTo - hostFrom) != rangeSize {
			return r, fmt.Errorf("Port %q's range size differs between host and destination at %s", e, path)
		}
		for d := 0; d <= rangeSize; d++ {
			targetPort := targetFrom + d
			pubPort := hostFrom + d
			if targetPort < 0 || targetPort > 65535 {
				return r, fmt.Errorf("Target port %d exceeded range", targetPort)
			}
			if pubPort < 0 || pubPort > 65535 {
				return r, fmt.Errorf("Published port %d exceeded range", pubPort)
			}
			r = append(r, PortBinding{uint16(targetPort), uint16(pubPort), prot})
		}
	}
	return r, nil
}

func toPortRange(rangeExpr string, path string) (from, to int, err error) {
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
	err = fmt.Errorf("Invalid port range %q at %s", rangeExpr, path)
	return
}

func toVolumeMounts(dcVols []string, sub Substitution, volDir, baseFile, destBaseFile string, r map[string]string, path string) (map[string]string, error) {
	if r == nil {
		r = map[string]string{}
	}
	if dcVols == nil {
		return r, nil
	}
	for _, e := range dcVols {
		e = sub(e)
		s := strings.SplitN(e, ":", 2)
		if len(s) != 2 {
			return nil, fmt.Errorf("Invalid volume entry %q at %s", e, path)
		}
		src := s[0]
		if len(src) == 0 || src[0] == '/' || src[0:2] == "./" || src[0:3] == "../" {
			// filesystem path
			src = translatePath(src, baseFile, destBaseFile)
		} else {
			// named volume reference
			src = filepath.Join(volDir, src)
		}
		r[s[1]] = src
	}
	return r, nil
}

func toImageBuild(s interface{}, sub Substitution, d *ImageBuild, baseFile, destBaseFile, path string) (r *ImageBuild, err error) {
	r = d
	switch s.(type) {
	case string:
		ctx := translatePath(sub(s.(string)), baseFile, destBaseFile)
		return &ImageBuild{ctx, "", nil}, nil
	case map[interface{}]interface{}:
		m := s.(map[interface{}]interface{})
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
		test, err := toStringArray(c.Test, sub, path)
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
			return nil, fmt.Errorf("%s: %v", err)
		}
		return &Check{cmd, interval, timeout, uint(retries), disable}, nil
	}
}

func toStringArray(v interface{}, sub Substitution, path string) ([]string, error) {
	switch v.(type) {
	case []interface{}:
		l := v.([]interface{})
		r := make([]string, len(l))
		for i, u := range l {
			r[i] = toString(u, sub, path)
		}
		return r, nil
	case string:
		return strings.Split(strings.Trim(sub(v.(string)), " "), " "), nil
	case nil:
		return []string{}, nil
	default:
		return nil, fmt.Errorf("string or []string expected at %s but was: %s", path, v)
	}
}

func toStringMap(v interface{}, sub Substitution, r map[string]string, path string) (map[string]string, error) {
	if r == nil {
		r = map[string]string{}
	}
	switch v.(type) {
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
			if len(s) != 2 {
				return r, fmt.Errorf("Invalid environment entry %q at %s", e, path)
			}
			r[s[0]] = s[1]
		}
		return r, nil
	case nil:
		return r, nil
	default:
		return nil, fmt.Errorf("map[string]string or []string expected at %s but was: %s", path, v)
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
	b, err := strconv.ParseBool(sub(s))
	if err != nil {
		return b, fmt.Errorf("%s: Cannot parse %q as bool", path, s)
	}
	return b, nil
}

func toString(v interface{}, sub Substitution, path string) string {
	switch v.(type) {
	case string:
		return sub(v.(string))
	case bool:
		return strconv.FormatBool(v.(bool))
	case int:
		return strconv.Itoa(v.(int))
	default:
		panic(fmt.Sprintf("String expected at %s", path))
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
	Image           string
	Build           interface{} // string or map[interface{}]interface{}
	Hostname        string
	Domainname      string
	Entrypoint      interface{}    // string or array
	Command         interface{}    // string or array
	WorkingDir      string         `yaml:"working_dir"`
	StdinOpen       string         `yaml:"stdin_open"`
	Tty             string         `yaml:"tty"`
	ReadOnly        string         `yaml:"read_only"`
	EnvFile         []string       `yaml:"env_file"`
	Environment     interface{}    // array of VAR=VAL or map
	HealthCheck     *dcHealthCheck `yaml:"healthcheck"`
	Expose          []string       `yaml:"expose"`
	Ports           []string       `yaml:"ports"`
	Volumes         []string       `yaml:"volumes"`
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
