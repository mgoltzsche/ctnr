package store

import (
	"strings"

	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/image"
)

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
