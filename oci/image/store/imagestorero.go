package store

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/oci/image"
	digest "github.com/opencontainers/go-digest"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

var _ image.ImageStoreRO = &ImageStoreRO{}

type ImageStoreRO struct {
	blobs    *BlobStoreExt
	imageDir string
	warn     log.Logger
}

func NewImageStoreRO(dir string, blobStore *BlobStoreExt, warn log.Logger) (r *ImageStoreRO, err error) {
	if err = os.MkdirAll(dir, 0755); err != nil {
		err = fmt.Errorf("init image store: %s", err)
		return
	}
	return &ImageStoreRO{blobStore, dir, warn}, err
}

func (s *ImageStoreRO) Image(id digest.Digest) (r image.Image, err error) {
	manifest, err := s.blobs.ImageManifest(id)
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	f, err := s.blobs.BlobFileInfo(id)
	if err != nil {
		return r, fmt.Errorf("image: %s", err)
	}
	return image.NewImage(id, "", "", f.ModTime(), manifest, nil, s.blobs), err
}

func (s *ImageStoreRO) ImageByName(nameRef string) (r image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("image %q not found in local store: %s", nameRef, err)
		}
	}()
	if id, e := digest.Parse(nameRef); e == nil && id.Validate() == nil {
		return s.Image(id)
	}
	name, ref := normalizeImageName(nameRef)
	var idx ispecs.Index
	if err = imageIndex(s.name2dir(name), &idx); err != nil {
		return
	}
	d, err := findManifestDigest(&idx, ref)
	if err != nil {
		return
	}
	return s.Image(d.Digest)
}

func (s *ImageStoreRO) Images() (r []image.Image, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("images: %s", err)
		}
	}()
	fl, err := ioutil.ReadDir(s.imageDir)
	if err != nil {
		return
	}
	r = make([]image.Image, 0, len(fl))
	var idx ispecs.Index
	var manifest ispecs.Manifest
	for _, f := range fl {
		if f.IsDir() {
			name := f.Name()
			name, e := s.dir2name(name)
			if e == nil {
				if e = imageIndex(s.name2dir(name), &idx); e == nil {
					for _, d := range idx.Manifests {
						ref := d.Annotations[ispecs.AnnotationRefName]
						if ref == "" {
							e = fmt.Errorf("image %q contains manifest descriptor without ref", name, d.Digest)
						} else {
							manifest, e = s.blobs.ImageManifest(d.Digest)
							if e == nil {
								r = append(r, image.NewImage(d.Digest, name, ref, f.ModTime(), manifest, nil, s.blobs))
							}
						}
					}
				}
			}
			if e != nil {
				s.warn.Printf("image %q: %s", name, e)
			}
		}
	}
	return
}

func (s *ImageStoreRO) name2dir(name string) string {
	name = base64.RawStdEncoding.EncodeToString([]byte(name))
	return filepath.Join(s.imageDir, name)
}

func (s *ImageStoreRO) dir2name(fileName string) (name string, err error) {
	b, err := base64.RawStdEncoding.DecodeString(fileName)
	if err == nil {
		name = string(b)
	} else {
		name = fileName
		err = fmt.Errorf("image name: %s", err)
	}
	return
}
