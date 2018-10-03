package store

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/image"
	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/mgoltzsche/cntnr/pkg/lock"
	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/pkg/errors"
)

// TODO: Move into imagerepo after repo lock is made optional
func imageIndex(dir string, r *ispecs.Index) (err error) {
	idxFile := filepath.Join(dir, "index.json")
	b, err := ioutil.ReadFile(idxFile)
	if err != nil {
		if os.IsNotExist(err) {
			return err
		}
		return errors.New("read image index: " + err.Error())
	}
	if err = json.Unmarshal(b, r); err != nil {
		err = errors.New("unmarshal image index " + idxFile + ": " + err.Error())
	}
	return
}

func normalizeImageName(nameAndTag string) *image.TagName {
	imgRef, err := alltransports.ParseImageName(nameAndTag)
	if err != nil {
		return parseImageName(nameAndTag)
	}
	return nameAndRef(imgRef)
}

func nameAndRef(imgRef types.ImageReference) *image.TagName {
	name := strings.TrimLeft(imgRef.StringWithinTransport(), "/")
	dckrRef := imgRef.DockerReference()
	if dckrRef != nil {
		name = dckrRef.String()
	}
	return parseImageName(name)
}

func parseImageName(nameAndRef string) *image.TagName {
	var r image.TagName
	if li := strings.LastIndex(nameAndRef, ":"); li > 0 && li+1 < len(nameAndRef) {
		r.Repo = nameAndRef[:li]
		r.Ref = nameAndRef[li+1:]
	} else {
		r.Repo = nameAndRef
		r.Ref = "latest"
	}
	return &r
}

func unlock(lock lock.Locker, err *error) {
	*err = exterrors.Append(*err, lock.Unlock())
}
