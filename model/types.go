package model

import (
	//specs "github.com/opencontainers/runtime-spec/specs-go"
	"encoding/json"
	"time"
)

type Project struct {
	Dir      string             `json:"-"`
	Services map[string]Service `json:"services"`
	Volumes  map[string]string  `json:"volumes,omitempty"`
}

type Service struct {
	Name       string      `json:"-"`
	Image      string      `json:"image,omitempty"`
	Build      *ImageBuild `json:"build,omitempty"`
	Hostname   string      `json:"hostname,omitempty"`
	Domainname string      `json:"domainname,omitempty"`
	// TODO: read dns, search, extra_hosts from docker compose
	Dns             []string          `json:"dns,omitempty"`
	DnsSearch       []string          `json:"dns_search,omitempty"`
	ExtraHosts      map[string]string `json:"extra_hosts,omitempty"`
	Entrypoint      []string          `json:"entrypoint,omitempty"`
	Command         []string          `json:"command,omitempty"`
	Cwd             string            `json:"working_dir,omitempty"`
	StdinOpen       bool              `json:"stdin_open,omitempty"`
	Tty             bool              `json:"tty,omitempty"`
	ReadOnly        bool              `json:"read_only,omitempty"`
	Environment     map[string]string `json:"environment,omitempty"`
	Expose          []string          `json:"expose,omitempty"`
	Ports           []PortBinding     `json:"ports,omitempty"`
	Mounts          map[string]string `json:"mounts,omitempty"`
	HealthCheck     *Check            `json:"healthcheck,omitempty"`
	SharedKeys      map[string]string `json:"shared_keys,omitempty"`
	StopSignal      string            `json:"stop_signal,omitempty"`
	StopGracePeriod time.Duration     `json:"stop_grace_period"`
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

type Volume struct {
	Source   string `json:"source"`
	Kind     string `json:"kind"`
	Readonly bool   `json:"readonly,omitempty"`
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

func (p *Project) JSON() string {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		panic("Failed to marshal effective model: " + err.Error())
	}
	return string(b)
}
