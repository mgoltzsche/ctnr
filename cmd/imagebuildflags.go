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

package cmd

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgoltzsche/cntnr/image"
	"github.com/mgoltzsche/cntnr/image/builder"
	"github.com/mgoltzsche/cntnr/image/builder/dockerfile"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/spf13/pflag"
)

var (
	flagProot         bool
	flagNoCache       bool
	flagImageBuildOps imageBuildFlags
	flagRm            bool
	flagRmAll         bool
)

type imageBuildFlags struct {
	ops           []func(*builder.ImageBuilder) error
	dockerfileCtx *dockerfileBuildContext
}

func (s *imageBuildFlags) add(op func(*builder.ImageBuilder) error) {
	s.ops = append(s.ops, op)
}

func initImageBuildFlags(f *pflag.FlagSet) {
	ops := &flagImageBuildOps
	f.Var((*iDockerfile)(ops), "dockerfile", "Builds the dockerfile at the provided path")
	f.Var((*iDockerfileTarget)(ops), "target", "Specifies the last --dockerfile's build target")
	f.Var((*iDockerfileArg)(ops), "build-arg", "Specifies the last --dockerfile's build arg")
	f.Var((*iFromImage)(ops), "from", "Extends the provided parent")
	f.Var((*iAuthor)(ops), "author", "Sets the new image's author")
	f.Var((*iLabel)(ops), "label", "Adds labels to the image")
	f.Var((*iEnv)(ops), "env", "Adds environment variables to the image")
	f.Var((*iWorkDir)(ops), "workdir", "Sets the new image's working directory")
	f.Var((*iEntrypoint)(ops), "entrypoint", "Sets the new image's entrypoint")
	f.Var((*iCmd)(ops), "cmd", "Sets the new image's command")
	f.Var((*iUser)(ops), "user", "Sets the new image's user")
	f.Var((*iRun)(ops), "run", "Runs the provided command in the current image")
	// TODO: remove?!
	f.Var((*iRunShell)(ops), "run-sh", "Runs the provided commands using a shell in the current image")
	f.Var((*iAdd)(ops), "add", "Adds glob pattern matching files to image: SRC... [DEST[:USER[:GROUP]]]")
	f.Var((*iTag)(ops), "tag", "Tags the image")
	f.BoolVar(&flagProot, "proot", false, "Enables PRoot")
	f.BoolVar(&flagNoCache, "no-cache", false, "Disables caches")
	f.BoolVar(&flagRm, "rm", true, "Remove intermediate containers after successful build")
	f.BoolVar(&flagRmAll, "force-rm", false, "Always remove containers after build")
}

type iFromImage imageBuildFlags

func (o *iFromImage) Set(image string) (err error) {
	err = checkNonEmpty(image)
	s := (*imageBuildFlags)(o)
	s.dockerfileCtx = nil
	s.add(func(b *builder.ImageBuilder) error {
		return b.FromImage(image)
	})
	return
}

func (o *iFromImage) Type() string {
	return "string"
}

func (o *iFromImage) String() string {
	return ""
}

type iDockerfile imageBuildFlags

func (o *iDockerfile) Set(file string) (err error) {
	err = checkNonEmpty(file)
	if err != nil {
		return
	}
	d, err := ioutil.ReadFile(file)
	if err != nil {
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	if wd, err = filepath.Abs(wd); err != nil {
		return
	}
	s := (*imageBuildFlags)(o)
	ctx := &dockerfileBuildContext{map[string]string{}, nil}
	s.dockerfileCtx = ctx
	s.add(func(b *builder.ImageBuilder) (err error) {
		// TODO: load dockerfile first and defer build only - when args can be provided after dockerfile has been loaded
		df, err := dockerfile.LoadDockerfile(d, wd, ctx.args, loggers.Warn)
		if err != nil {
			return
		}
		for _, target := range ctx.targets {
			if err = df.Target(target); err != nil {
				return
			}
		}
		b.SetImageResolver(builder.ResolveDockerImage)
		defer b.SetImageResolver(image.GetImage)
		return df.Apply(b)
	})
	return
}

type dockerfileBuildContext struct {
	args    map[string]string
	targets []string
}

func (o *iDockerfile) Type() string {
	return "string"
}

func (o *iDockerfile) String() string {
	return ""
}

type iDockerfileTarget imageBuildFlags

func (o *iDockerfileTarget) Set(target string) (err error) {
	err = checkNonEmpty(target)
	if err != nil {
		return
	}
	s := (*imageBuildFlags)(o)
	if s.dockerfileCtx == nil {
		return usageError("--dockerfile option must be specified first")
	}
	s.dockerfileCtx.targets = append(s.dockerfileCtx.targets, target)
	return
}

func (o *iDockerfileTarget) Type() string {
	return "string"
}

func (o *iDockerfileTarget) String() string {
	return ""
}

type iDockerfileArg imageBuildFlags

func (o *iDockerfileArg) Set(kv string) (err error) {
	err = checkNonEmpty(kv)
	if err != nil {
		return
	}
	s := (*imageBuildFlags)(o)
	if s.dockerfileCtx == nil {
		return usageError("--dockerfile option must be specified first")
	}
	return addMapEntries(kv, &s.dockerfileCtx.args)
}

func (o *iDockerfileArg) Type() string {
	return "string"
}

func (o *iDockerfileArg) String() string {
	return ""
}

type iRun imageBuildFlags

func (o *iRun) Set(cmd string) (err error) {
	if err = checkNonEmpty(cmd); err != nil {
		return
	}
	p, err := parseStringEntries(cmd)
	if err != nil {
		return
	}
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.Run(p, nil)
	})
	return
}

func (o *iRun) Type() string {
	return "string"
}

func (o *iRun) String() string {
	return ""
}

type iRunShell imageBuildFlags

func (o *iRunShell) Set(cmd string) (err error) {
	if err = checkNonEmpty(cmd); err != nil {
		return
	}
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.Run([]string{"/bin/sh", "-c", cmd}, nil)
	})
	return
}

func (o *iRunShell) Type() string {
	return "string"
}

func (o *iRunShell) String() string {
	return ""
}

type iAdd imageBuildFlags

// TODO: support passing src image
func (o *iAdd) Set(expr string) (err error) {
	l, err := parseStringEntries(expr)
	if err != nil {
		return
	}
	var srcPattern []string
	dest := ""
	var usr *idutils.User
	switch len(l) {
	case 0:
		return usageError("no source file pattern provided")
	case 1:
		srcPattern = []string{l[0]}
	default:
		srcPattern = l[0 : len(l)-1]
		dest = l[len(l)-1]
		destParts := strings.Split(dest, ":")
		dest = destParts[0]
		usrstr := strings.Join(destParts[1:], ":")
		if usrstr != "" {
			user := idutils.ParseUser(usrstr)
			usr = &user
		}
	}
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.AddFiles(".", srcPattern, dest, usr)
	})
	return
}

func (o *iAdd) Type() string {
	return "string"
}

func (o *iAdd) String() string {
	return ""
}

type iUser imageBuildFlags

func (o *iUser) Set(user string) (err error) {
	err = checkNonEmpty(user)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.SetUser(user)
	})
	return
}

func (o *iUser) Type() string {
	return "string"
}

func (o *iUser) String() string {
	return ""
}

type iAuthor imageBuildFlags

func (o *iAuthor) Set(author string) (err error) {
	err = checkNonEmpty(author)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.SetAuthor(author)
	})
	return
}

func (o *iAuthor) Type() string {
	return "string"
}

func (o *iAuthor) String() string {
	return ""
}

type iEnv imageBuildFlags

func (o *iEnv) Set(v string) (err error) {
	env := map[string]string{}
	if err = addMapEntries(v, &env); err == nil && len(env) == 0 {
		err = usageError("no environment variables provided (expecting KEY=VAL ...)")
	}
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.AddEnv(env)
	})
	return
}

func (o *iEnv) Type() string {
	return "string"
}

func (o *iEnv) String() string {
	return ""
}

type iWorkDir imageBuildFlags

func (o *iWorkDir) Set(dir string) (err error) {
	err = checkNonEmpty(dir)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.SetWorkingDir(dir)
	})
	return
}

func (o *iWorkDir) Type() string {
	return "string"
}

func (o *iWorkDir) String() string {
	return ""
}

type iEntrypoint imageBuildFlags

func (o *iEntrypoint) Set(s string) (err error) {
	entrypoint := make([]string, 0, 1)
	err = addStringEntries(s, &entrypoint)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.SetEntrypoint(entrypoint)
	})
	return
}

func (o *iEntrypoint) Type() string {
	return "string"
}

func (o *iEntrypoint) String() string {
	return ""
}

type iCmd imageBuildFlags

func (o *iCmd) Set(s string) (err error) {
	cmd := make([]string, 0, 1)
	err = addStringEntries(s, &cmd)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.SetCmd(cmd)
	})
	return
}

func (o *iCmd) Type() string {
	return "string"
}

func (o *iCmd) String() string {
	return ""
}

type iLabel imageBuildFlags

func (o *iLabel) Set(v string) (err error) {
	labels := map[string]string{}
	if err = addMapEntries(v, &labels); err == nil && len(labels) == 0 {
		err = usageError("no labels provided (expecting KEY=VAL ...)")
	}
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.AddLabels(labels)
	})
	return
}

func (o *iLabel) Type() string {
	return "KEY=VALUE"
}

func (o *iLabel) String() string {
	return ""
}

type iTag imageBuildFlags

func (o *iTag) Set(tag string) (err error) {
	err = checkNonEmpty(tag)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.Tag(tag)
	})
	return
}

func (o *iTag) Type() string {
	return "string"
}

func (o *iTag) String() string {
	return ""
}
