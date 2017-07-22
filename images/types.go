package images

import (
	"github.com/containers/image/signature"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	spec "github.com/opencontainers/image-spec/specs-go/v1"
)

type PullPolicy string

const (
	PULL_NEVER  PullPolicy = "never"
	PULL_NEW    PullPolicy = "new"
	PULL_UPDATE PullPolicy = "update"
)

type Image struct {
	Name      string         `json:"name"`
	Directory string         `json:"directory"`
	Index     *spec.Index    `json:"index"`
	Manifest  *spec.Manifest `json:"manifest"`
	Config    *spec.Image    `json:"config"`
}

type Images struct {
	images         map[string]*Image
	imageDirectory string
	trustPolicy    *signature.PolicyContext
	pullPolicy     PullPolicy
	context        *types.SystemContext
	debug          log.Logger
}
