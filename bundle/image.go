package bundle

import (
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

type BundleImage interface {
	ID() digest.Digest
	// Returns the image's configuration - never nil
	Config() *ispecs.Image
	Unpack(dest string) error
}
