package fs

import (
	"io"
)

type Source interface {
	// TODO: a) Replace with CalcDerivedAttrs() and Attrs() returning pointer to all attrs?!
	//          or at least let DerivedAttrs() return only derived attrs without normal attrs
	Attrs() NodeInfo
	Write(dest, name string, w Writer, written map[Source]string) error
	// TODO: b) Remove and compare all attributes within FsNode after Attrs() returns all and CalcDerivedAttrs() is called
	Equal(other Source) (bool, error)
}

type BlobSource interface {
	Source
	DerivedAttrs() (NodeAttrs, error)
}

type FileSource interface {
	BlobSource
	Readable
	HashIfAvailable() string
}

type LazySource interface {
	BlobSource
	Expand(dest string, w Writer, written map[Source]string) error
}

type Readable interface {
	Reader() (io.ReadCloser, error)
}
