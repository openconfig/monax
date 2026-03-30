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
	"errors"
	"fmt"

	"github.com/openconfig/monax"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:           "stop",
	Short:         "Stop SUT",
	SilenceErrors: true,
	RunE:          stopRunE,
}

func stopRunE(cmd *cobra.Command, _ []string) error {
	if err := sut.Stop(cmd.Context()); err != nil {
		var sutErr *monax.SUTError
		if errors.As(err, &sutErr) {
			return fmt.Errorf("stop SUT: %s", sutErr.PrettyPrint())
		}
		return fmt.Errorf("stop SUT: %s", err)
	}
	fmt.Println("SUT stopped successfully.")
	return nil
}
