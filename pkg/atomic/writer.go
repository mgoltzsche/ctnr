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

package atomic

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
)

// Writes a file atomically by first writing into a temp file before moving it to its final destination
func WriteFile(dest string, reader io.Reader) (size int64, err error) {
	// Create temp file to write blob to
	tmpFile, err := ioutil.TempFile(filepath.Dir(dest), ".tmp-")
	if err != nil {
		return 0, errors.New(err.Error())
	}
	tmpPath := tmpFile.Name()
	tmpRemoved := false
	defer func() {
		tmpFile.Close()
		if !tmpRemoved {
			os.Remove(tmpPath)
		}
	}()

	// Write temp file
	if size, err = io.Copy(tmpFile, reader); err != nil {
		err = errors.New("write to temp file: " + err.Error())
		return
	}
	if err = tmpFile.Sync(); err != nil {
		err = errors.New("sync temp file: " + err.Error())
		return
	}
	if err = tmpFile.Close(); err != nil {
		err = errors.New("close temp file: " + err.Error())
		return
	}

	// Rename temp file
	if err = os.Rename(tmpPath, dest); err != nil {
		return 0, errors.New("rename temp file: " + err.Error())
	}
	tmpRemoved = true
	return
}

// Writes a JSON file atomically by first writing into a temp file before moving it to its final destination
func WriteJson(dest string, o interface{}) (size int64, err error) {
	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(o); err != nil {
		return 0, errors.New("write json: " + err.Error())
	}
	return WriteFile(dest, &buf)
}
