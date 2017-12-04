package bundle

import (
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type BundleImage interface {
	ID() digest.Digest
	Config() (ispecs.Image, error)
	Unpack(dest string) error
}
