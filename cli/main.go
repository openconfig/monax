// Copyright 2026 Google LLC
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

// The monaxctl binary is for users to interact with Monax SUTs in an
// interactive shell for the lifetime of the SUT.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/context"

	"flag"

	log "github.com/golang/glog"
	"github.com/openconfig/monax"
	"github.com/openconfig/monax/runtime/kubernetesruntime"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Global variables.
var (
	// Required at top level to set Monax Config flags.
	config monax.Config

	// Used by all commands. There can only ever be one SUT per CLI session.
	sut *monax.SUT

	// Flags
	verbose     bool
	runtimeType string
)

func init() {
	fs := flag.CommandLine
	fs.BoolVar(&verbose, "verbose", false, "Enable verbose logging of component details")
	config.RegisterFlags(fs)
}

func main() {
	flag.Parse()
	ctx := context.Background()
	if verbose {
		flag.Set("v", "1")
		flag.Set("alsologtostderr", "true")
	}

	newRuntimeFn := kubernetesruntime.New

	var err error
	fmt.Println("Initializing Monax SUT...")
	sut, err = monax.New(ctx, &config, newRuntimeFn)
	if err != nil {
		log.ExitContextf(ctx, "Failed to create Monax SUT: %v", err)
	}

	rootCmd := &cobra.Command{
		Use:          "monax",
		SilenceUsage: true,
	}
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(exitCmd)
	rootCmd.AddCommand(targetsCmd)
	rootCmd.AddCommand(verboseCmd)

	fmt.Println("Monax REPL. Type 'help' for commands, 'exit' to quit.")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\nmonax> ")
		// Disable logging while waiting for commands as async logs aren't helpful
		// and cause the CLI prompt line to disappear.
		flag.Set("alsologtostderr", "false")
		if !scanner.Scan() {
			if scanner.Err() == nil { // EOF
				fmt.Println("exit")
				return
			}
			log.ExitContextf(ctx, "Scan error: %v", scanner.Err())
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		args := monaxSuggest(strings.Fields(line), rootCmd)
		rootCmd.SetArgs(args)

		// Enable logging for the command execution to see logs from Monax lib.
		flag.Set("alsologtostderr", "true")
		err := rootCmd.Execute()
		if errors.Is(err, ErrExit) {
			return
		}
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		}
		resetFlags(rootCmd)
	}
}

// monaxSuggest returns the best matching command name for the given line.
//
// This is needed because Cobra only interacts with the terminal's returned line
// input (user presses Enter). This means if a user started typing a new command
// before the previous command completed, only newly typed characters appear,
// which can be confusing when a valid looking command fails.
//
// For example, user accidentally hit some random keys while "start" command is
// still running and then when the command completes, they type "status", a
// valid command, but the user sees "unknown command" error with all characters.
func monaxSuggest(args []string, rootCmd *cobra.Command) []string {
	bestMatch := func(args []string, index int, bestCmdName string) []string {
		return append([]string{bestCmdName}, args[index+1:]...)
	}

	for i, arg := range args {
		// "help" is not in rootCmd.Commands() but is supported.
		if strings.HasSuffix(arg, "help") {
			return bestMatch(args, i, "help")
		}
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() != "" && strings.HasSuffix(arg, cmd.Name()) {
				return bestMatch(args, i, cmd.Name())
			}
			for _, alias := range cmd.Aliases {
				if strings.HasSuffix(arg, alias) {
					return bestMatch(args, i, alias)
				}
			}
		}
	}

	return args
}

// resetFlags sets all Monax commands' flags back to default values.
//
// This prevents the CLI from getting stuck in an infinite loop of always using
// the last used flag value.
func resetFlags(cmd *cobra.Command) {
	for _, childCmd := range cmd.Commands() {
		resetFlags(childCmd)
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Value.Set(f.DefValue)
	})
}
