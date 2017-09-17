package oci

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

func writeFile(dest string, contents []byte) error {
	// Create temp file
	tmpFile, err := ioutil.TempFile(filepath.Dir(dest), "tmp-")
	if err != nil {
		return fmt.Errorf("create temp file: %s", err)
	}
	defer tmpFile.Close()
	tmpName := tmpFile.Name()

	// Write temp file
	if _, err = io.Copy(io.Writer(tmpFile), bytes.NewReader(contents)); err != nil {
		return fmt.Errorf("write temp file: %s", err)
	}
	tmpFile.Close()

	// Rename temp file
	if err = os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("rename temp file: %s", err)
	}
	return nil
}
