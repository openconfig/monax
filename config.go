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

package monax

import (
	"cmp"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"flag"

	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	monaxpb "github.com/openconfig/monax/proto"
)

var (
	// ErrDecodeTextproto indicates a textproto could not be decoded to a specific
	// protobuf message.
	ErrDecodeTextproto = errors.New("decode textproto")

	// ErrNoAbstractSUT indicates an abstract SUT was not set in the Config.
	ErrNoAbstractSUT = errors.New("no abstract SUT in config")

	// ErrNoLibrary indicates a library was not set in the Config.
	ErrNoLibrary = errors.New("no library in config")

	// ErrReadTextproto indicates a textproto file could not be read.
	ErrReadTextproto = errors.New("read textproto")
)

var (
	readAbstractSUTFn       = readAbstractSUT
	readLibraryFn           = readLibrary
	readRuntimeParametersFn = readRuntimeParameters
)

// A Config contains the parameters used to create a new SUT.
//
// The parameters can be set programmatically.  Some parameters may be set with
// command line flags, as in the following example.
//
//	package main
//
//	var config monax.Config
//
//	func init() {
//		config.RegisterFlags(nil) // install the flags
//	}
//
//	func main() {
//		flag.Parse()
//		ctx := context.Background()
//		runtime := ...
//		sut, err := monax.New(ctx, &config, runtime)
//		// check err and use sut ...
//	}
type Config struct {
	// An abstract SUT is a description of the high-level requirements of a test.
	//
	// One of AbstractSUT or AbstractSUTPath must be set in the config.  If both
	// are set, AbstractSUT will be used and AbstractSUTPath will be ignored.
	// AbstractSUTPath is a path to a monax.AbstractSut textproto and may be set
	// from the command line.
	AbstractSUT     *monaxpb.AbstractSut
	AbstractSUTPath string

	// A library contains descriptions of the components available to satisfy the
	// requirements of the abstract SUT.
	//
	// One of Library or LibraryPath must be set in the config.  If both are set,
	// Library will be used and LibraryPath will be ignored. LibraryPath is a path
	// to a monax.Library textproto and may be set from the command line.
	Library     *monaxpb.Library
	LibraryPath string

	// Runtime parameters are additional configuration options for the runtime.
	//
	// RuntimeParameters or RuntimeParametersPath may be set in the config.  If
	// both are set, RuntimeParameters will be used and RuntimeParametersPath will
	// be ignored.  RuntimeParametersPath is a path to a monax.RuntimeParameters
	// and may be set from the command line.
	RuntimeParameters     *monaxpb.RuntimeParameters
	RuntimeParametersPath string

	// componentBaseDirByID is a map from component ID to the directory that paths
	// in the component should be relative to.  This is usually the directory that
	// contains the library that contains the component.
	//
	// NOTE: Component IDs must be globally unique in Monax, but that requirement
	// will not be verified until after this map is populated.  We do not bother
	// checking for collisions in this map and instead wait for processLibrary to
	// catch them later.
	componentBaseDirByID map[string]string
}

// RegisterFlags registers command line flags that modify a Config.
// flag.CommandLine is used when the flag set is nil.
func (c *Config) RegisterFlags(fs *flag.FlagSet) {
	fs = cmp.Or(fs, flag.CommandLine)

	fs.StringVar(&c.AbstractSUTPath, "abstract_sut", "", "Path to the Monax abstract SUT file")

	fs.StringVar(&c.LibraryPath, "library", "", "Path to the Monax library file")

	fs.StringVar(&c.RuntimeParametersPath, "runtime_parameters", "", "Path to the Monax runtime parameters file")
}

func readConfig(config *Config) error {
	if err := readAbstractSUTFn(config); err != nil {
		return fmt.Errorf("read abstract SUT: %w", err)
	}
	if err := readLibraryFn(config); err != nil {
		return fmt.Errorf("read library: %w", err)
	}
	if err := readRuntimeParametersFn(config); err != nil {
		return fmt.Errorf("read runtime parameters: %w", err)
	}
	return nil
}

func readAbstractSUT(config *Config) error {
	if config.AbstractSUT == nil && config.AbstractSUTPath == "" {
		return ErrNoAbstractSUT
	}
	if config.AbstractSUT == nil {
		config.AbstractSUT = new(monaxpb.AbstractSut)
		if err := unmarshal(config.AbstractSUTPath, config.AbstractSUT); err != nil {
			return err
		}
	}
	return nil
}

// resolveLibraries reads the library at the path, injects textproto paths into
// components, appends the components to the components in the baseLibrary, and
// recursively does the same for referenced libraries.
func resolveLibraries(config *Config, libraryPath string) error {
	currentLibrary := new(monaxpb.Library)
	if err := unmarshal(libraryPath, currentLibrary); err != nil {
		return err
	}

	// Add current components to base library.
	libraryDir := filepath.Dir(libraryPath)
	for _, component := range currentLibrary.GetComponents() {
		config.componentBaseDirByID[component.GetId()] = libraryDir
		config.Library.SetComponents(append(config.Library.GetComponents(), component))
	}

	// Do some path management and recurse for all the referenced libraries.
	for _, nextLibraryPath := range currentLibrary.GetLibraries() {
		if !filepath.IsAbs(nextLibraryPath) {
			nextLibraryPath = filepath.Join(libraryDir, nextLibraryPath)
		}
		if err := resolveLibraries(config, nextLibraryPath); err != nil {
			return err
		}
	}

	return nil
}

func readLibrary(config *Config) error {
	if config.Library == nil && config.LibraryPath == "" {
		return ErrNoLibrary
	}
	if config.Library == nil {
		config.Library = new(monaxpb.Library)
		config.componentBaseDirByID = make(map[string]string)
		if err := resolveLibraries(config, config.LibraryPath); err != nil {
			return err
		}
	}
	return nil
}

func readRuntimeParameters(config *Config) error {
	if config.RuntimeParameters == nil && config.RuntimeParametersPath == "" {
		return nil
	}
	if config.RuntimeParameters == nil {
		config.RuntimeParameters = new(monaxpb.RuntimeParameters)
		if err := unmarshal(config.RuntimeParametersPath, config.RuntimeParameters); err != nil {
			return err
		}
	}
	return nil
}

func unmarshal(path string, message proto.Message) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrReadTextproto, path, err)
	}
	if err := prototext.Unmarshal(b, message); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrDecodeTextproto, path, err)
	}
	return nil
}
