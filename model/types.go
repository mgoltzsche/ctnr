package model

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	MOUNT_TYPE_VOLUME = MountType("volume")
	MOUNT_TYPE_BIND   = MountType("bind")
	MOUNT_TYPE_TMPFS  = MountType("tmpfs")
)

func FromJSON(b []byte) (r CompoundServices, err error) {
	if err = json.Unmarshal(b, &r); err != nil {
		err = errors.Wrap(err, "unmarshal CompoundServices")
	}
	for k, s := range r.Services {
		// TODO: better use slice instead of map for services to avoid copying service struct in such cases
		s.Name = k
		r.Services[k] = s
	}
	return
}

type CompoundServices struct {
	Dir      string             `json:"-"`
	Services map[string]Service `json:"services"`
	Volumes  map[string]Volume  `json:"volumes,omitempty"`
}

type Service struct {
	Name         string `json:"-"`
	Bundle       string `json:"bundle,omitempty"`
	BundleUpdate bool   `json:"bundle_update,omitempty"`
	NoPivot      bool   `json:"no_pivot,omitempty"`
	NoNewKeyring bool   `json:"no_new_keyring,omitempty"`

	Image string      `json:"image,omitempty"`
	Build *ImageBuild `json:"build,omitempty"`
	Process
	Seccomp      string `json:"seccomp,omitempty"`
	MountCgroups string `json:"cgroups_mount_option,omitempty"` // Not read from compose file. TODO: move to CLI only
	NetConf
	ReadOnly bool          `json:"read_only,omitempty"`
	Expose   []string      `json:"expose,omitempty"`
	Volumes  []VolumeMount `json:"volumes,omitempty"`
	// TODO: handle check
	HealthCheck     *Check         `json:"healthcheck,omitempty"`
	StopSignal      string         `json:"stop_signal,omitempty"`
	StopGracePeriod *time.Duration `json:"stop_grace_period,omitempty"`

	// TODO: uid/gid mapping: spec.AddLinuxUIDMapping(hostid, containerid, size), ... AddLinuxGIDMapping
}

type Process struct {
	Entrypoint  []string          `json:"entrypoint,omitempty"`
	Command     []string          `json:"command,omitempty"`
	PRoot       bool              `json:"proot,omitempty"`
	Cwd         string            `json:"working_dir,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	User        *User             `json:"user,omitempty"`
	CapAdd      []string          `json:"cap_add,omitempty"`
	CapDrop     []string          `json:"cap_drop,omitempty"`
	StdinOpen   bool              `json:"stdin_open,omitempty"`
	Tty         bool              `json:"tty,omitempty"`
	// TODO: ConsoleSocket string            `json:"console_socket,omitempty"`

	// TODO: expose in CLI
	ApparmorProfile string `json:"app_armor_profile,omitempty"`
	SelinuxLabel    string `json:"process_label,omitempty"`
	NoNewPrivileges bool   `json:"no_new_privileges,omitempty"`
	OOMScoreAdj     *int   `json:"oom_score_adj,omitempty"`
	//TODO:
	//Rlimits         []Rlimit `json:"rlimit,omitempty"`
	//ExtraFiles []*os.File
}

type Rlimit struct {
	Type string `json:"type"`
	Hard uint64 `json:"hard"`
	Soft uint64 `json:"soft"`
}

type User struct {
	User  string `json:"uid,omitempty"`
	Group string `json:"gid,omitempty"`
	// TODO: expose in CLI
	AdditionalGroups []string `json:"additional_groups,omitempty"`
}

type NetConf struct {
	Hostname   string        `json:"hostname,omitempty"`
	Domainname string        `json:"domainname,omitempty"`
	Dns        []string      `json:"dns,omitempty"`
	DnsSearch  []string      `json:"dns_search,omitempty"`
	DnsOptions []string      `json:"dns_options,omitempty"`
	ExtraHosts []ExtraHost   `json:"extra_hosts,omitempty"`
	Ports      []PortBinding `json:"ports,omitempty"`
	Networks   []string      `json:"networks,omitempty"`
}

type ExtraHost struct {
	Name string `json:"name"`
	Ip   string `json:"ip"`
}

type ImageBuild struct {
	Context    string            `json:"context,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
}

type PortBinding struct {
	Target    uint16 `json:"target"`
	Published uint16 `json:"published,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	IP        string `json:"ip,omitempty"`
}

func (p PortBinding) String() string {
	var s string
	pub := p.Published
	if pub == 0 {
		pub = p.Target
	}
	if p.IP == "" {
		s = strconv.Itoa(int(pub)) + ":"
	} else {
		s = p.IP + ":" + strconv.Itoa(int(pub)) + ":"
	}
	s += strconv.Itoa(int(p.Target))
	if p.Protocol != "" && p.Protocol != "tcp" {
		s += "/" + p.Protocol
	}
	return s
}

type VolumeMount struct {
	Type    MountType `json:"type,omitempty"`
	Source  string    `json:"source,omitempty"`
	Target  string    `json:"target,omitempty"`
	Options []string  `json:"options,omitempty"`
}

type MountType string

func (m VolumeMount) IsNamedVolume() bool {
	src := m.Source
	return len(src) > 0 && !(src[0] == '~' || src[0] == '/' || src[0] == '.')
}

func (m VolumeMount) String() string {
	s := "type=" + string(m.Type)
	if m.Source != "" {
		s += ",src=" + m.Source
	}
	s += ",dst=" + m.Target
	for _, o := range m.Options {
		s += ",opt=" + o
	}
	return s
}

// Parses a docker-like mount expression, see https://docs.docker.com/storage/bind-mounts/#choosing-the--v-or-mount-flag
func ParseMount(expr string) (r VolumeMount, err error) {
	// Check bind mount format
	if strings.Index(expr, "=") == -1 || (strings.Index(expr, ",") == -1 && strings.Index(expr, ":") != -1) {
		if r, err = ParseBindMount(expr); err != nil {
			err = errors.Errorf("invalid mount expression %q")
		}
		return
	}

	// Parse kv pairs
	r.Type = MOUNT_TYPE_BIND
	r.Options = []string{}
	for _, e := range strings.Split(expr, ",") {
		kv := strings.SplitN(e, "=", 2)
		k := strings.ToLower(strings.Trim(kv[0], " "))
		v := ""
		if len(kv) == 2 {
			v = strings.Trim(kv[1], " ")
		}
		switch {
		case k == "type":
			r.Type = MountType(v)
		case k == "source" || k == "src":
			r.Source = v
		case k == "destination" || k == "dst" || k == "target":
			r.Target = v
		case k == "readonly":
			r.Options = append(r.Options, "ro")
		case k == "volume-opt" || k == "opt":
			r.Options = append(r.Options, v)
		default:
			return r, errors.Errorf("unsupported mount key %q", k)
		}
	}
	if r.Type == "" {
		if r.Source == "" {
			return r, errors.New("no mount type specified")
		}
		if r.IsNamedVolume() {
			r.Type = "volume"
		} else {
			r.Type = "bind"
		}
	}
	if r.Target == "" {
		err = errors.Errorf("no volume mount target specified but %q", expr)
	}
	return
}

func ParseBindMount(expr string) (r VolumeMount, err error) {
	r.Type = MOUNT_TYPE_BIND
	r.Options = []string{}
	s := strings.Split(expr, ":")
	switch len(s) {
	case 0:
	case 1:
		r.Source = ""
		r.Target = s[0]
	default:
		r.Source = s[0]
		r.Target = s[1]
		r.Options = s[2:]
	}
	if r.Target == "" {
		err = errors.Errorf("no volume mount target specified but %q", expr)
	}
	return
}

type Volume struct {
	Source   string `json:"source,omitempty"`
	External string `json:"external,omitempty"` // Name of an external volume
}

type Check struct {
	Command []string `json:"cmd"`
	//Http     string        `json:"http"`
	Interval *time.Duration `json:"interval"`
	Timeout  *time.Duration `json:"timeout"`
	Retries  uint           `json:"retries,omitempty"`
	Disable  bool           `json:"disable,omitempty"`
}

func NewService(name string) Service {
	return Service{Name: name, Seccomp: "default"}
}

func (c *CompoundServices) JSON() string {
	return toJSON(c)
}

func (c *Service) JSON() string {
	return toJSON(c)
}

func toJSON(o interface{}) string {
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		panic("Failed to marshal effective model: " + err.Error())
	}
	return string(b)
}
