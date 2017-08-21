package model

import (
	"encoding/json"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"time"
)

type VolumeResolver interface {
	PrepareVolumeMount(VolumeMount) (specs.Mount, error)
}

type Project struct {
	Dir      string             `json:"-"`
	Services map[string]Service `json:"services"`
	Volumes  map[string]Volume  `json:"volumes,omitempty"`
}

type Service struct {
	Name        string            `json:"-"`
	Image       string            `json:"image,omitempty"`
	Build       *ImageBuild       `json:"build,omitempty"`
	Hostname    string            `json:"hostname,omitempty"`
	Domainname  string            `json:"domainname,omitempty"`
	Dns         []string          `json:"dns,omitempty"`
	DnsSearch   []string          `json:"dns_search,omitempty"`
	ExtraHosts  []ExtraHost       `json:"extra_hosts,omitempty"`
	Entrypoint  []string          `json:"entrypoint,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Cwd         string            `json:"working_dir,omitempty"`
	StdinOpen   bool              `json:"stdin_open,omitempty"`
	Tty         bool              `json:"tty,omitempty"`
	ReadOnly    bool              `json:"read_only,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Expose      []string          `json:"expose,omitempty"`
	// TODO: bind ports
	Ports   []PortBinding `json:"ports,omitempty"`
	Volumes []VolumeMount `json:"volumes,omitempty"`
	// TODO: handle check
	HealthCheck     *Check        `json:"healthcheck,omitempty"`
	StopSignal      string        `json:"stop_signal,omitempty"`
	StopGracePeriod time.Duration `json:"stop_grace_period"`
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
	Published uint16 `json:"published"`
	Protocol  string `json:"protocol"`
}

type VolumeMount struct {
	Type    string   `json:"type,omitempty"`
	Source  string   `json:"source,omitempty"`
	Target  string   `json:"target,omitempty"`
	Options []string `json:"options,omitempty"`
}

func (m VolumeMount) IsNamedVolume() bool {
	src := m.Source
	return len(src) > 0 && !(src[0] == '~' || src[0] == '/' || src[0] == '.')
}

type Volume struct {
	Source   string `json:"source,omitempty"`
	External string `json:"external,omitempty"` // Name of an external volume
}

type Check struct {
	Command []string `json:"cmd"`
	//Http     string        `json:"http"`
	Interval time.Duration `json:"interval"`
	Timeout  time.Duration `json:"timeout"`
	Retries  uint          `json:"retries,omitempty"`
	Disable  bool          `json:"disable,omitempty"`
}

func NewService(name string) *Service {
	return &Service{Name: name, StopGracePeriod: time.Duration(10000000000)}
}

func (c *Project) JSON() string {
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
