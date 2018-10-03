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
	"os"
	"time"

	exterrors "github.com/mgoltzsche/cntnr/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	gcCmd = &cobra.Command{
		Use:   "gc",
		Short: "Garage collects all bundles and images in the local store",
		Long:  `Garage collects all bundles and images in the local store.`,
		Run:   wrapRun(runGc),
	}
	flagGcBundleTTL time.Duration
	flagGcImageTTL  time.Duration
)

func init() {
	gcCmd.Flags().DurationVarP(&flagGcBundleTTL, "bundle-ttl", "b", defaultBundleTTL, "bundle lifetime before it gets garbage collected")
	gcCmd.Flags().DurationVarP(&flagImageTTL, "image-ttl", "i", defaultImageTTL, "image lifetime before it gets garbage collected")
}

func runGc(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError("No args expected")
	}
	gcd, err := store.BundleGC(flagGcBundleTTL)
	for _, b := range gcd {
		os.Stdout.WriteString(b.ID() + "\n")
	}
	return exterrors.Append(err, store.ImageGC(flagGcImageTTL))
}
