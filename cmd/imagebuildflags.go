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
	"strings"

	"github.com/mgoltzsche/cntnr/image/builder"
	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/spf13/pflag"
)

var (
	flagProot         bool
	flagNoCache       bool
	flagImageBuildOps imageBuildFlags
)

type imageBuildFlags struct {
	ops []func(*builder.ImageBuilder) error
}

func (s *imageBuildFlags) add(op func(*builder.ImageBuilder) error) {
	s.ops = append(s.ops, op)
}

func initImageBuildFlags(f *pflag.FlagSet) {
	ops := &flagImageBuildOps
	f.Var((*iFromImage)(ops), "from", "Extends the provided parent image (must come first)")
	f.Var((*iAuthor)(ops), "author", "Sets the new image's author")
	f.Var((*iEnv)(ops), "env", "Adds environment variables to the image")
	f.Var((*iWorkDir)(ops), "workdir", "Sets the new image's working directory")
	f.Var((*iEntrypoint)(ops), "entrypoint", "Sets the new image's entrypoint")
	f.Var((*iCmd)(ops), "cmd", "Sets the new image's command")
	f.Var((*iUser)(ops), "user", "Sets the new image's user")
	f.Var((*iRun)(ops), "run", "Runs the provided command in the current image")
	f.Var((*iAdd)(ops), "add", "Adds glob pattern matching files to image: SRC... [DEST[:USER[:GROUP]]]")
	f.Var((*iTag)(ops), "tag", "Tags the image")
	f.BoolVar(&flagProot, "proot", false, "Enables PRoot")
	f.BoolVar(&flagNoCache, "no-cache", false, "Disables caches")
}

type iRun imageBuildFlags

func (o *iRun) Set(cmd string) (err error) {
	err = checkNonEmpty(cmd)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
		return b.Run(cmd)
	})
	return
}

func (o *iRun) Type() string {
	return "string"
}

func (o *iRun) String() string {
	return ""
}

type iAdd imageBuildFlags

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
		return b.CopyFile("", srcPattern, dest, usr)
	})
	return
}

func (o *iAdd) Type() string {
	return "string"
}

func (o *iAdd) String() string {
	return ""
}

type iFromImage imageBuildFlags

func (o *iFromImage) Set(image string) (err error) {
	err = checkNonEmpty(image)
	(*imageBuildFlags)(o).add(func(b *builder.ImageBuilder) error {
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
