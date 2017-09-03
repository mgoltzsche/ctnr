// Copyright © 2017 Max Goltzsche
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bundles

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/cntnr/model"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type Bundle struct {
	ID  string
	Dir string
	*specs.Spec
}

func (b *Bundle) ImageName() string {
	return b.Annotations[model.ANNOTATION_BUNDLE_IMAGE_NAME]
}

func (b *Bundle) Created() string {
	return b.Annotations[model.ANNOTATION_BUNDLE_CREATED]
}

type Bundles struct {
	storeDir string
}

func NewBundles(storeDir string) (*Bundles, error) {
	if _, err := os.Stat(storeDir); os.IsNotExist(err) {
		if err = os.MkdirAll(storeDir, 0770); err != nil {
			return nil, err
		}
	}
	return &Bundles{storeDir}, nil
}

func (self *Bundles) Dir() string {
	return self.storeDir
}

func (self *Bundles) List() (r []*Bundle, err error) {
	r = []*Bundle{}
	files, err := ioutil.ReadDir(self.storeDir)
	if err != nil {
		return
	}
	for _, f := range files {
		bundleDir := filepath.Join(self.storeDir, f.Name())
		bundleConfFile := filepath.Join(bundleDir, "config.json")
		if _, e := os.Stat(bundleConfFile); !os.IsNotExist(e) {
			s, err := LoadBundleConfig(bundleDir)
			if err == nil {
				r = append(r, &Bundle{f.Name(), bundleDir, s})
			} else {
				os.Stderr.WriteString(fmt.Sprintf("Cannot read bundle %s: %s\n", bundleDir, err))
			}
		}
	}
	return
}

func (self *Bundles) Delete(id string) (err error) {
	return os.RemoveAll(filepath.Join(self.storeDir, id))
}

func LoadBundleConfig(bundleDir string) (s *specs.Spec, err error) {
	b, err := ioutil.ReadFile(filepath.Join(bundleDir, "config.json"))
	if err != nil {
		return
	}
	s = &specs.Spec{}
	err = json.Unmarshal(b, s)
	return
}
