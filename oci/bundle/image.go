package bundle

import (
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type BundleImage interface {
	ID() string
	Config() (ispecs.Image, error)
	Unpack(dest string) error
}
