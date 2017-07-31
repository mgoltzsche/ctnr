package net

import (
	"encoding/json"
	"fmt"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"io/ioutil"
	"os"
	"path/filepath"
)

func loadBundleSpec(bundleDir string) (*specs.Spec, error) {
	spec := &specs.Spec{}
	f, err := os.Open(filepath.Join(bundleDir, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("Cannot open runtime bundle spec: %v", err)
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("Cannot read runtime bundle spec: %v", err)
	}
	if err := json.Unmarshal(b, spec); err != nil {
		return nil, fmt.Errorf("Cannot unmarshal runtime bundle spec: %v", err)
	}

	return spec, nil
}

func writeFile(dest, content string) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte(content)); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
