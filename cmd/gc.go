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

	"github.com/spf13/cobra"
)

var (
	gcCmd = &cobra.Command{
		Use:   "gc",
		Short: "Garage collects all bundles in the bundle store",
		Long:  `Garage collects all bundles in the bundle store.`,
		Run:   handleError(runBundleGc),
	}
	bundleTTL time.Duration
)

func init() {
	gcCmd.Flags().DurationVarP(&bundleTTL, "ttl", "t", time.Duration(1000*1000*1000*60*30 /*30min*/), "bundle lifetime before it gets garbage collected")
}

func runBundleGc(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usageError("No args expected")
	}
	gcd, err := store.BundleGC(time.Now().Add(-bundleTTL))
	for _, b := range gcd {
		os.Stdout.WriteString(b.ID + "\n")
	}
	return err
}
