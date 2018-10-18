package fs

import (
	"github.com/golang/protobuf/proto"
	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/openSUSE/umoci/pkg/rootlesscontainers-proto"
	"github.com/pkg/errors"
)

type AttrMapper interface {
	ToContainer(a *FileAttrs) error
	ToHost(a *FileAttrs) error
}

// An AttrMapper that maps uids/gids using rootlesscontainers xattr.
// See https://github.com/rootless-containers/proto
// and https://github.com/openSUSE/umoci/blob/master/oci/layer/utils.go
type rootlessAttrMapper struct {
	idMap idutils.IdMappings
}

func NewRootlessAttrMapper(idMap idutils.IdMappings) AttrMapper {
	return &rootlessAttrMapper{idMap}
}

func (s rootlessAttrMapper) ToContainer(a *FileAttrs) (err error) {
	if a.Xattrs != nil {
		if xattrUser, ok := a.Xattrs[rootlesscontainers.Keyname]; ok {
			a.UserIds = idutils.UserIds{}
			var usr rootlesscontainers.Resource
			if err = proto.Unmarshal([]byte(xattrUser), &usr); err != nil {
				return errors.Wrap(err, "uid/gid from xattr "+rootlesscontainers.Keyname)
			}
			if uid := usr.GetUid(); uid != rootlesscontainers.NoopID {
				a.UserIds.Uid = uint(uid)
			}
			if gid := usr.GetGid(); gid != rootlesscontainers.NoopID {
				a.UserIds.Gid = uint(gid)
			}
			delete(a.Xattrs, rootlesscontainers.Keyname)
			return
		}
	}
	a.UserIds = idutils.UserIds{0, 0}
	return
}

func (s rootlessAttrMapper) ToHost(a *FileAttrs) (err error) {
	usr := rootlesscontainers.Resource{rootlesscontainers.NoopID, rootlesscontainers.NoopID}
	if uid := a.Uid; uid != 0 {
		usr.Uid = uint32(a.Uid)
	}
	if gid := a.Gid; gid != 0 {
		usr.Gid = uint32(a.Gid)
	}
	if !rootlesscontainers.IsDefault(usr) {
		var xattrVal []byte
		if xattrVal, err = proto.Marshal(&usr); err != nil {
			return errors.Wrap(err, "uid/gid to xattr "+rootlesscontainers.Keyname)
		}
		if a.Xattrs == nil {
			a.Xattrs = map[string]string{}
		}
		a.Xattrs[rootlesscontainers.Keyname] = string(xattrVal)
	}
	usrIds := idutils.UserIds{0, 0}
	a.UserIds, err = usrIds.ToHost(s.idMap)
	return
}

type rootAttrMapper struct {
	idMap idutils.IdMappings
}

func NewAttrMapper(idMap idutils.IdMappings) AttrMapper {
	return &rootAttrMapper{idMap}
}

func (s *rootAttrMapper) ToContainer(a *FileAttrs) (err error) {
	a.UserIds, err = a.UserIds.ToContainer(s.idMap)
	return
}

func (s *rootAttrMapper) ToHost(a *FileAttrs) (err error) {
	a.UserIds, err = a.UserIds.ToHost(s.idMap)
	return nil
}
