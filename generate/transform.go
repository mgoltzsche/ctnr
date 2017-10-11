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

package generate

import (
	"strings"

	ispecs "github.com/opencontainers/image-spec/specs-go/v1"
	rspecs "github.com/opencontainers/runtime-spec/specs-go"

	"time"

	"github.com/opencontainers/runtime-tools/generate"
)

type SpecBuilder struct {
	generate.Generator
	entrypoint []string
	cmd        []string
}

func NewSpecBuilder() SpecBuilder {
	return SpecBuilder{generate.New(), nil, nil}
}

func FromSpec(spec *rspecs.Spec) SpecBuilder {
	return SpecBuilder{generate.NewFromSpec(spec), nil, nil}
}

func (b *SpecBuilder) SetProcessEntrypoint(v []string) {
	b.entrypoint = v
	b.cmd = nil
	b.apply()
}

func (b *SpecBuilder) SetProcessCmd(v []string) {
	b.cmd = v
	b.apply()
}

func (b *SpecBuilder) apply() {
	if b.entrypoint != nil || b.cmd != nil {
		if b.entrypoint != nil && b.cmd != nil {
			b.SetProcessArgs(append(b.entrypoint, b.cmd...))
		} else if b.entrypoint != nil {
			b.SetProcessArgs(b.entrypoint)
		} else {
			b.SetProcessArgs(b.cmd)
		}
	} else {
		b.SetProcessArgs([]string{})
	}
}

func (b *SpecBuilder) ApplyImage(img ispecs.Image) {
	cfg := &img.Config

	// Entrypoint
	b.SetProcessEntrypoint(cfg.Entrypoint)
	b.SetProcessEntrypoint(cfg.Cmd)

	// Env
	if len(cfg.Env) > 0 {
		for _, e := range cfg.Env {
			kv := strings.SplitN(e, "=", 2)
			k := kv[0]
			v := ""
			if len(kv) == 2 {
				v = kv[1]
			}
			b.AddProcessEnv(k, v)
		}
	}

	// Working dir
	if cfg.WorkingDir != "" {
		b.SetProcessCwd(cfg.WorkingDir)
	}

	// Annotations
	if cfg.Labels != nil {
		for k, v := range cfg.Labels {
			b.AddAnnotation(k, v)
		}
	}
	// TODO: extract annotations also from image index and manifest
	if img.Author != "" {
		b.AddAnnotation("org.opencontainers.image.author", img.Author)
	}
	if !time.Unix(0, 0).Equal(*img.Created) {
		b.AddAnnotation("org.opencontainers.image.created", (*img.Created).String())
	}
	if img.Config.StopSignal != "" {
		b.AddAnnotation("org.opencontainers.image.stopSignal", img.Config.StopSignal)
	}
	if cfg.ExposedPorts != nil {
		ports := make([]string, len(cfg.ExposedPorts))
		i := 0
		for k := range cfg.ExposedPorts {
			ports[i] = k
			i++
		}
		b.AddAnnotation("org.opencontainers.image.exposedPorts", strings.Join(ports, ","))
	}
}
