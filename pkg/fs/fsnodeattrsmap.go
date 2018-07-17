package fs

import (
	"os"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	//"github.com/openSUSE/umoci/pkg/rootlesscontainers-proto" - requires new version
)

var (
	RootlessAttrMapper AttrMapper = rootlessAttrMapper("rootless-attr-mapper")
)

type AttrMapper interface {
	ToContainer(a *FileAttrs) error
	ToHost(a *FileAttrs) error
}

// An AttrMapper that maps uids/gids using user.rootlesscontainers xattr.
// See https://github.com/rootless-containers/proto
type rootlessAttrMapper string

func (s rootlessAttrMapper) ToContainer(a *FileAttrs) (err error) {
	// TODO: read uid/gid from xattr user.rootlesscontainers
	a.UserIds = idutils.UserIds{0, 0}
	return
}

func (s rootlessAttrMapper) ToHost(a *FileAttrs) (err error) {
	if !a.UserIds.IsZero() {
		if a.Xattrs == nil {
			a.Xattrs = map[string]string{}
		}
		// TODO: set xattr user.rootlesscontainers => uid/gid using protobuf
	}
	a.UserIds = idutils.UserIds{uint(os.Geteuid()), uint(os.Getegid())}
	return nil
}

type RootAttrMapper struct {
	idMap idutils.IdMappings
}

func NewAttrMapper(idMap idutils.IdMappings) *RootAttrMapper {
	return &RootAttrMapper{idMap}
}

func (s *RootAttrMapper) ToContainer(a *FileAttrs) (err error) {
	a.UserIds, err = a.UserIds.ToContainer(s.idMap)
	return
}

func (s *RootAttrMapper) ToHost(a *FileAttrs) (err error) {
	a.UserIds, err = a.UserIds.ToHost(s.idMap)
	return nil
}
