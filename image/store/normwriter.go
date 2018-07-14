package store

import (
	"bytes"
	"io"
)

type mtreeNormWriter struct {
	w io.Writer
}

func normWriter(w io.Writer) io.Writer {
	return &mtreeNormWriter{w}
}

func (w *mtreeNormWriter) Write(b []byte) (n int, err error) {
	lines := bytes.Split(b, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) > 0 && line[0] != '#' {
			l := 0
			if l, err = w.w.Write(append(line, []byte("\n")...)); err != nil {
				return
			}
			n += l
		}
	}
	return
}

/*func MtreeBytes(dh *mtree.DirectoryHierarchy) []byte {
	var buf bytes.Buffer
	dh.WriteTo(&buf)
	b := buf.Bytes()
	normalized := make([]byte, len(b))
	lines := bytes.Split(b, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) > 0 && line[0] != '#' {
			normalized = append(append(normalized, line...), []byte("\n")...)
		}
	}
	return normalized
}
*/
