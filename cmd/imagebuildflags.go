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
	"github.com/spf13/pflag"

	"github.com/mgoltzsche/cntnr/oci/image/builder"
)

func initImageBuildFlags(f *pflag.FlagSet, imageBuilder *builder.ImageBuilder) {
	f.Var((*bFromImage)(imageBuilder), "from", "Extends the provided parent image (must come first)")
	f.Var((*bAuthor)(imageBuilder), "author", "Sets the new image's author")
	f.Var((*bWorkingDir)(imageBuilder), "work", "Sets the new image's working directory")
	f.Var((*bEntrypoint)(imageBuilder), "entrypoint", "Sets the new image's entrypoint")
	f.Var((*bCmd)(imageBuilder), "cmd", "Sets the new image's command")
	f.Var((*bRun)(imageBuilder), "run", "Creates a new image by running the provided command in the current image")
	f.Var((*bTag)(imageBuilder), "tag", "Tags the image")
}

type bRun builder.ImageBuilder

func (b *bRun) Set(cmd string) (err error) {
	(*builder.ImageBuilder)(b).Run(cmd)
	return
}

func (b *bRun) Type() string {
	return "string"
}

func (b *bRun) String() string {
	return ""
}

type bFromImage builder.ImageBuilder

func (b *bFromImage) Set(image string) (err error) {
	(*builder.ImageBuilder)(b).FromImage(image)
	return
}

func (b *bFromImage) Type() string {
	return "string"
}

func (b *bFromImage) String() string {
	return ""
}

type bAuthor builder.ImageBuilder

func (b *bAuthor) Set(author string) (err error) {
	(*builder.ImageBuilder)(b).SetAuthor(author)
	return
}

func (b *bAuthor) Type() string {
	return "string"
}

func (b *bAuthor) String() string {
	return ""
}

type bWorkingDir builder.ImageBuilder

func (b *bWorkingDir) Set(s string) (err error) {
	(*builder.ImageBuilder)(b).SetWorkingDir(s)
	return
}

func (b *bWorkingDir) Type() string {
	return "string"
}

func (b *bWorkingDir) String() string {
	return ""
}

type bEntrypoint builder.ImageBuilder

func (b *bEntrypoint) Set(s string) (err error) {
	entrypoint := make([]string, 0, 1)
	if err = addStringEntries(s, &entrypoint); err != nil {
		return
	}
	(*builder.ImageBuilder)(b).SetEntrypoint(entrypoint)
	return
}

func (b *bEntrypoint) Type() string {
	return "string"
}

func (b *bEntrypoint) String() string {
	return ""
}

type bCmd builder.ImageBuilder

func (b *bCmd) Set(s string) (err error) {
	cmd := make([]string, 0, 1)
	if err = addStringEntries(s, &cmd); err != nil {
		return
	}
	(*builder.ImageBuilder)(b).SetCmd(cmd)
	return
}

func (b *bCmd) Type() string {
	return "string"
}

func (b *bCmd) String() string {
	return ""
}

type bTag builder.ImageBuilder

func (b *bTag) Set(tag string) (err error) {
	(*builder.ImageBuilder)(b).Tag(tag)
	return
}

func (b *bTag) Type() string {
	return "string"
}

func (b *bTag) String() string {
	return ""
}
