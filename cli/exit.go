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

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
)

// ErrExit signals that the CLI should exit.
var ErrExit = errors.New("exit")

var exitCmd = &cobra.Command{
	Use:           "exit",
	Short:         "Exit REPL",
	SilenceErrors: true,
	RunE:          exitRunE,
}

func exitRunE(cmd *cobra.Command, _ []string) error {
	if !sut.Started() {
		// SUT is not started, safe to exit.
		return ErrExit
	}

	// SUT is running. Prompt the user to stop the SUT.
	confirm := false
	prompt := &survey.Confirm{
		Message: "SUT is still running. Are you sure you want to exit?",
	}
	if err := survey.AskOne(prompt, &confirm); err != nil {
		return fmt.Errorf("could not prompt for exit: %w", err)
	}
	if confirm {
		return ErrExit
	}
	return nil
}
