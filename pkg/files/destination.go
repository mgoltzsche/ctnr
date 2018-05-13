package files

import (
	"path/filepath"
)

type CopyPair struct {
	Source string
	Dest   string
}

func Map(src []string, dest string) []CopyPair {
	r := make([]CopyPair, len(src))
	for i, file := range src {
		file = filepath.Clean(file)
		destDir := dest
		destFile := filepath.Base(file)
		if len(src) == 1 && len(dest) > 0 && dest[len(dest)-1] != '/' {
			// Use dest as file name without appending src file name
			// if there is only one source file and dest does not end with '/'
			destDir, destFile = filepath.Split(dest)
		}
		destFile = filepath.Join(destDir, destFile)
		r[i] = CopyPair{file, filepath.Clean("/" + destFile)}
	}
	return r
}

/* DOES NOT WORK SINCE mtree.Walk cannot walk on single file:

var hashKeywords = []mtree.Keyword{
	"size",
	"type",
	"uid",
	"gid",
	"mode",
	"link",
	"nlink",
	"sha256digest",
	"xattr",
}

func Hash(cpPairs []CopyPair, rootless bool) (d digest.Digest, err error) {
	sort.Sort(byDest(cpPairs))
	fsEval := fseval.DefaultFsEval
	if rootless {
		fsEval = fseval.RootlessFsEval
	}
	var buf bytes.Buffer
	for _, p := range cpPairs {
		var dh *mtree.DirectoryHierarchy
		if dh, err = mtree.Walk(p.Source, nil, hashKeywords, fsEval); err != nil {
			err = errors.New("hash cp pairs: " + err.Error())
			break
		}
		buf.WriteString("#" + p.Dest + "\n")
		dh.WriteTo(&buf)
	}
	return digest.FromBytes(buf.Bytes()), err
}

type byDest []CopyPair

func (p byDest) Len() int           { return len(p) }
func (p byDest) Less(i, j int) bool { return p[i].Dest < p[j].Dest }
func (p byDest) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
*/
