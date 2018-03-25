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
	"github.com/mgoltzsche/cntnr/image/builder"
	"github.com/spf13/pflag"
)

var (
	flagProot   bool
	flagNoCache bool
)

func initImageBuildFlags(f *pflag.FlagSet, imageBuilder *builder.ImageBuilder) {
	f.Var((*iFromImage)(imageBuilder), "from", "Extends the provided parent image (must come first)")
	f.Var((*iAuthor)(imageBuilder), "author", "Sets the new image's author")
	f.Var((*iWorkingDir)(imageBuilder), "work", "Sets the new image's working directory")
	f.Var((*iEntrypoint)(imageBuilder), "entrypoint", "Sets the new image's entrypoint")
	f.Var((*iCmd)(imageBuilder), "cmd", "Sets the new image's command")
	f.Var((*iRun)(imageBuilder), "run", "Runs the provided command in the current image")
	f.Var((*iAdd)(imageBuilder), "add", "Adds glob pattern matching files to image: SRCPATTERN... [DEST]")
	f.Var((*iTag)(imageBuilder), "tag", "Tags the image")
	f.BoolVar(&flagProot, "proot", false, "Enables PRoot")
	f.BoolVar(&flagNoCache, "no-cache", false, "Disables caches")
}

type iRun builder.ImageBuilder

func (b *iRun) Set(cmd string) (err error) {
	(*builder.ImageBuilder)(b).Run(cmd)
	return
}

func (b *iRun) Type() string {
	return "string"
}

func (b *iRun) String() string {
	return ""
}

type iAdd builder.ImageBuilder

func (b *iAdd) Set(expr string) (err error) {
	l, err := parseStringEntries(expr)
	if err != nil {
		return
	}
	var srcPattern []string
	dest := ""
	switch len(l) {
	case 0:
		return usageError("no value provided (expecting SRC... [DEST])")
	case 1:
		srcPattern = []string{l[0]}
	default:
		srcPattern = l[0 : len(l)-1]
		dest = l[len(l)-1]
	}
	return (*builder.ImageBuilder)(b).Copy("", srcPattern, dest)
}

func (b *iAdd) Type() string {
	return "string"
}

func (b *iAdd) String() string {
	return ""
}

type iFromImage builder.ImageBuilder

func (b *iFromImage) Set(image string) (err error) {
	(*builder.ImageBuilder)(b).FromImage(image)
	return
}

func (b *iFromImage) Type() string {
	return "string"
}

func (b *iFromImage) String() string {
	return ""
}

type iAuthor builder.ImageBuilder

func (b *iAuthor) Set(author string) (err error) {
	(*builder.ImageBuilder)(b).SetAuthor(author)
	return
}

func (b *iAuthor) Type() string {
	return "string"
}

func (b *iAuthor) String() string {
	return ""
}

type iWorkingDir builder.ImageBuilder

func (b *iWorkingDir) Set(s string) (err error) {
	(*builder.ImageBuilder)(b).SetWorkingDir(s)
	return
}

func (b *iWorkingDir) Type() string {
	return "string"
}

func (b *iWorkingDir) String() string {
	return ""
}

type iEntrypoint builder.ImageBuilder

func (b *iEntrypoint) Set(s string) (err error) {
	entrypoint := make([]string, 0, 1)
	if err = addStringEntries(s, &entrypoint); err != nil {
		return
	}
	(*builder.ImageBuilder)(b).SetEntrypoint(entrypoint)
	return
}

func (b *iEntrypoint) Type() string {
	return "string"
}

func (b *iEntrypoint) String() string {
	return ""
}

type iCmd builder.ImageBuilder

func (b *iCmd) Set(s string) (err error) {
	cmd := make([]string, 0, 1)
	if err = addStringEntries(s, &cmd); err != nil {
		return
	}
	(*builder.ImageBuilder)(b).SetCmd(cmd)
	return
}

func (b *iCmd) Type() string {
	return "string"
}

func (b *iCmd) String() string {
	return ""
}

type iTag builder.ImageBuilder

func (b *iTag) Set(tag string) (err error) {
	(*builder.ImageBuilder)(b).Tag(tag)
	return
}

func (b *iTag) Type() string {
	return "string"
}

func (b *iTag) String() string {
	return ""
}
