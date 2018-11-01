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

	exterrors "github.com/mgoltzsche/ctnr/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	gcCmd = &cobra.Command{
		Use:   "gc",
		Short: "Garage collects all bundles and images in the local store",
		Long:  `Garage collects all bundles and images in the local store.`,
		Run:   wrapRun(runGc),
	}
	flagGcBundleTTL        time.Duration
	flagGcImageTTL         time.Duration
	flagGcImageRefTTL      time.Duration
	flagGcMaxImagesPerRepo int
)

func init() {
	gcCmd.Flags().DurationVarP(&flagGcBundleTTL, "bundle-ttl", "b", defaultBundleTTL, "bundle lifetime before it gets garbage collected")
	gcCmd.Flags().DurationVarP(&flagGcImageTTL, "image-ttl", "i", defaultImageTTL, "image lifetime before it gets garbage collected")
	gcCmd.Flags().DurationVarP(&flagGcImageRefTTL, "ref-ttl", "r", 0, "tagged image lifetime before it gets garbage collected")
	gcCmd.Flags().IntVarP(&flagGcMaxImagesPerRepo, "max", "m", 0, "max entries per repo (default 0 == unlimited)")
}

func runGc(cmd *cobra.Command, args []string) (err error) {
	if len(args) > 0 {
		return usageError("No args expected")
	}
	cm, err := newContainerManager()
	if err != nil {
		return
	}
	gcd, err := store.BundleGC(flagGcBundleTTL, cm)
	for _, b := range gcd {
		os.Stdout.WriteString(b.ID() + "\n")
	}
	return exterrors.Append(err, store.ImageGC(flagGcImageTTL, flagGcImageRefTTL, flagGcMaxImagesPerRepo))
}
