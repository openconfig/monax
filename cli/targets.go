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

// Package main is the entry point for the Monax CLI.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var targetsCmd = &cobra.Command{
	Use:   "targets endpoint_type service_name[s]",
	Short: "Get SUT component connection information",
	Long: "Get SUT component connection information (default: gRPC)\n\n" +
		"The service names and available endpoint types are defined in the " +
		"Abstract SUT textproto file.",
	SilenceErrors: true,
}

func init() {
	targetsCmd.AddCommand(targetsDHCPCmd)
	targetsCmd.AddCommand(targetsGRPCCmd)
	targetsCmd.AddCommand(targetsHTTPCmd)
	targetsCmd.AddCommand(targetsHTTPSCmd)
}

var targetsDHCPCmd = &cobra.Command{
	Use:           "dhcp service_name [service_name2 ...]",
	Short:         "Get SUT component DHCP connection information",
	SilenceErrors: true,
	Run:           runTargetsDHCP,
	Args:          cobra.MinimumNArgs(1),
}

var targetsGRPCCmd = &cobra.Command{
	Use:           "grpc service_name [service_name2 ...]",
	Short:         "Get SUT component gRPC connection information",
	SilenceErrors: true,
	Run:           runTargetsGRPC,
	Args:          cobra.MinimumNArgs(1),
}

var targetsHTTPCmd = &cobra.Command{
	Use:           "http service_name [service_name2 ...]",
	Short:         "Get SUT component HTTP connection information",
	SilenceErrors: true,
	Run:           runTargetsHTTP,
	Args:          cobra.MinimumNArgs(1),
}

var targetsHTTPSCmd = &cobra.Command{
	Use:           "https service_name [service_name2 ...]",
	Short:         "Get SUT component HTTPS connection information",
	SilenceErrors: true,
	Run:           runTargetsHTTPS,
	Args:          cobra.MinimumNArgs(1),
}

func printTargetOrError(arg, target string, err error) {
	// if/else so all targets are printed even if some have errors.
	if err != nil {
		fmt.Println(fmt.Sprintf("%s: %v", arg, err))
	} else {
		fmt.Println(fmt.Sprintf("%s: %s", arg, target))
	}
}

func runTargetsDHCP(cmd *cobra.Command, args []string) {
	targets := sut.Targets()
	for _, arg := range args {
		target, err := targets.DHCP(cmd.Context(), arg)
		printTargetOrError(arg, target, err)
	}
}

func runTargetsGRPC(cmd *cobra.Command, args []string) {
	targets := sut.Targets()
	for _, arg := range args {
		target, err := targets.GRPC(cmd.Context(), arg)
		printTargetOrError(arg, target, err)
	}
}

func runTargetsHTTP(cmd *cobra.Command, args []string) {
	targets := sut.Targets()
	for _, arg := range args {
		target, err := targets.HTTP(cmd.Context(), arg)
		printTargetOrError(arg, target, err)
	}
}

func runTargetsHTTPS(cmd *cobra.Command, args []string) {
	targets := sut.Targets()
	for _, arg := range args {
		target, err := targets.HTTPS(cmd.Context(), arg)
		printTargetOrError(arg, target, err)
	}
}
