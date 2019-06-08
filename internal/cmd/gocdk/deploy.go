// Copyright 2019 The Go Cloud Development Kit Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"

	"github.com/spf13/cobra"
)

func registerDeployCmd(ctx context.Context, pctx *processContext, rootCmd *cobra.Command) {
	deployCmd := &cobra.Command{
		Use:   "deploy BIOME",
		Short: "TODO Deploy the biome",
		Long:  "TODO more about deploy",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			biome := args[0]
			if err := build(ctx, pctx, defaultDockerTag); err != nil {
				return err
			}
			if err := apply(ctx, pctx, biome, true); err != nil {
				return err
			}
			if err := launch(ctx, pctx, biome, defaultDockerTag); err != nil {
				return err
			}
			return nil
		},
	}
	rootCmd.AddCommand(deployCmd)
}
