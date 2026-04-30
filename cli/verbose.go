// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"fmt"
	"strconv"

	"flag"

	"github.com/spf13/cobra"
)

var verboseCmd = &cobra.Command{
	Use:           "verbose [true|false]",
	Short:         "Set verbose logging",
	SilenceErrors: true,
	RunE:          verboseRunE,
}

func verboseRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		verbose = !verbose
	} else {
		b, err := strconv.ParseBool(args[0])
		if err != nil {
			return fmt.Errorf("invalid argument: %v", args[0])
		}
		verbose = b
	}

	// Set the flag value for the logging library to read.
	if verbose {
		flag.Set("v", "1")
	} else {
		flag.Set("v", "0")
	}

	fmt.Println("Verbose set to:", verbose)
	return nil
}
