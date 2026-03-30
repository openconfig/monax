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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"flag"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	monaxpb "github.com/openconfig/monax/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

var (
	configDiffOpts = []cmp.Option{
		cmp.AllowUnexported(Config{}),
		cmpopts.EquateEmpty(),
		cmpopts.IgnoreFields(Config{}, "AbstractSUTPath", "LibraryPath", "RuntimeParametersPath"),
		protocmp.Transform(),
	}
)

func TestRegisterFlags(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		args       []string
		wantConfig *Config
	}{
		"none": {
			wantConfig: new(Config),
		},
		"abstract sut": {
			args: []string{
				"--abstract_sut=abstract_sut.txtpb",
			},
			wantConfig: &Config{
				AbstractSUTPath: "abstract_sut.txtpb",
			},
		},
		"library": {
			args: []string{
				"--library=library.txtpb",
			},
			wantConfig: &Config{
				LibraryPath: "library.txtpb",
			},
		},
		"runtime parameters": {
			args: []string{
				"--runtime_parameters=runtime_parameters.txtpb",
			},
			wantConfig: &Config{
				RuntimeParametersPath: "runtime_parameters.txtpb",
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := flag.NewFlagSet("test_flagset", flag.ContinueOnError)
			config := new(Config)
			config.RegisterFlags(fs)

			if err := fs.Parse(test.args); err != nil {
				t.Fatalf("fs.Parse(%v) returned unexpected error: %v", test.args, err)
			}
			if diff := cmp.Diff(test.wantConfig, config, configDiffOpts...); diff != "" {
				t.Errorf("RegisterFlags: config diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadAbstractSUT(t *testing.T) {
	t.Parallel()

	abstractSUT := monaxpb.AbstractSut_builder{
		RequiredInterfaces: []*monaxpb.Interface{
			monaxpb.Interface_builder{
				Grpc: monaxpb.Grpc_builder{
					ServiceName: proto.String("test.A"),
				}.Build(),
			}.Build(),
		},
	}.Build()
	abstractSUTPath := marshal(t, "abstract_sut.txtpb", abstractSUT)
	notAbstractSUTPath := marshal(t, "not_abstract_sut.txtpb", monaxpb.Component_builder{
		Id: proto.String("bad"),
	}.Build())

	tests := map[string]struct {
		config          *Config
		wantAbstractSUT *monaxpb.AbstractSut
		wantErr         error
	}{
		"no abstract sut": {
			config:  new(Config),
			wantErr: ErrNoAbstractSUT,
		},
		"missing abstract sut textproto": {
			config: &Config{
				AbstractSUTPath: "💀", // bogus path
			},
			wantErr: ErrReadTextproto,
		},
		"bad abstract sut textproto": {
			config: &Config{
				AbstractSUTPath: notAbstractSUTPath,
			},
			wantErr: ErrDecodeTextproto,
		},
		"pass with textproto": {
			config: &Config{
				AbstractSUTPath: abstractSUTPath,
			},
			wantAbstractSUT: abstractSUT,
		},
		"pass with proto": {
			config: &Config{
				AbstractSUT: abstractSUT,
			},
			wantAbstractSUT: abstractSUT,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := readAbstractSUT(test.config)

			if !errors.Is(err, test.wantErr) {
				t.Errorf("readAbstractSUT: error diff: want %v, got %v", test.wantErr, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.wantAbstractSUT, test.config.AbstractSUT, protocmp.Transform()); diff != "" {
				t.Errorf("readAbstractSUT: components diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadLibrary(t *testing.T) {
	t.Parallel()

	libraryDir := t.TempDir()
	library := monaxpb.Library_builder{
		Components: []*monaxpb.Component{
			monaxpb.Component_builder{
				Id: proto.String("A"),
			}.Build(),
		},
	}.Build()
	libraryPath := marshalAt(t, libraryDir, "library.txtpb", library)

	recursiveDir := t.TempDir()
	library2 := monaxpb.Library_builder{
		Components: []*monaxpb.Component{
			monaxpb.Component_builder{
				Id: proto.String("B"),
			}.Build(),
		},
	}.Build()
	marshalAt(t, recursiveDir, "library2.txtpb", library2)
	library3 := monaxpb.Library_builder{
		Components: []*monaxpb.Component{
			monaxpb.Component_builder{
				Id: proto.String("C"),
			}.Build(),
		},
	}.Build()
	marshalAt(t, recursiveDir, "tmp/library3.txtpb", library3)
	recursiveLibrary := monaxpb.Library_builder{
		Components: []*monaxpb.Component{
			monaxpb.Component_builder{
				Id: proto.String("A"),
			}.Build(),
		},
		Libraries: []string{"library2.txtpb", "tmp/library3.txtpb"},
	}.Build()
	recursiveLibraryPath := marshalAt(t, recursiveDir, "recursive_library.txtpb", recursiveLibrary)
	recursiveLibraryResolved := monaxpb.Library_builder{
		Components: []*monaxpb.Component{
			monaxpb.Component_builder{
				Id: proto.String("A"),
			}.Build(),
			monaxpb.Component_builder{
				Id: proto.String("B"),
			}.Build(),
			monaxpb.Component_builder{
				Id: proto.String("C"),
			}.Build(),
		},
	}.Build()

	notLibraryPath := marshal(t, "not_library.txtpb", monaxpb.Component_builder{
		Id: proto.String("bad"),
	}.Build())

	tests := map[string]struct {
		config     *Config
		wantConfig *Config
		wantErr    error
	}{
		"no library": {
			config:  new(Config),
			wantErr: ErrNoLibrary,
		},
		"missing library textproto": {
			config: &Config{
				LibraryPath: "💀", // bogus path
			},
			wantErr: ErrReadTextproto,
		},
		"bad library textproto": {
			config: &Config{
				LibraryPath: notLibraryPath,
			},
			wantErr: ErrDecodeTextproto,
		},
		"pass with textproto": {
			config: &Config{
				LibraryPath: libraryPath,
			},
			wantConfig: &Config{
				Library: library,
				componentBaseDirByID: map[string]string{
					"A": libraryDir,
				},
			},
		},
		"pass with proto": {
			config: &Config{
				Library: library,
			},
			wantConfig: &Config{
				Library: library,
			},
		},
		"pass with recursive library": {
			config: &Config{
				LibraryPath: recursiveLibraryPath,
			},
			wantConfig: &Config{
				Library: recursiveLibraryResolved,
				componentBaseDirByID: map[string]string{
					"A": recursiveDir,
					"B": recursiveDir,
					"C": filepath.Join(recursiveDir, "tmp"),
				},
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := readLibrary(test.config)

			if !errors.Is(err, test.wantErr) {
				t.Errorf("readLibrary: error diff: want %v, got %v", test.wantErr, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.config, test.wantConfig, configDiffOpts...); diff != "" {
				t.Errorf("readLibrary: config diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadRuntimeParameters(t *testing.T) {
	t.Parallel()

	runtimeParameters := monaxpb.RuntimeParameters_builder{
		Parameters: &anypb.Any{},
	}.Build()
	runtimeParametersPath := marshal(t, "runtime_parameters.txtpb", runtimeParameters)
	notRuntimeParametersPath := marshal(t, "not_runtime_parameters.txtpb", monaxpb.Component_builder{
		Id: proto.String("bad"),
	}.Build())

	tests := map[string]struct {
		config                *Config
		wantRuntimeParameters *monaxpb.RuntimeParameters
		wantErr               error
	}{
		"missing runtime parameters textproto": {
			config: &Config{
				RuntimeParametersPath: "💀", // bogus path
			},
			wantErr: ErrReadTextproto,
		},
		"bad runtime parameters textproto": {
			config: &Config{
				RuntimeParametersPath: notRuntimeParametersPath,
			},
			wantErr: ErrDecodeTextproto,
		},
		"pass with no runtime parameters": {
			config:                new(Config),
			wantRuntimeParameters: nil,
		},
		"pass with textproto": {
			config: &Config{
				RuntimeParametersPath: runtimeParametersPath,
			},
			wantRuntimeParameters: runtimeParameters,
		},
		"pass with proto": {
			config: &Config{
				RuntimeParameters: runtimeParameters,
			},
			wantRuntimeParameters: runtimeParameters,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := readRuntimeParameters(test.config)

			if !errors.Is(err, test.wantErr) {
				t.Errorf("readRuntimeParameters: error diff: want %v, got %v", test.wantErr, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.wantRuntimeParameters, test.config.RuntimeParameters, protocmp.Transform()); diff != "" {
				t.Errorf("readRuntimeParameters: components diff (-want +got):\n%s", diff)
			}
		})
	}
}

func marshal(t *testing.T, path string, message proto.Message) string {
	t.Helper()
	base := t.TempDir()
	return marshalAt(t, base, path, message)
}

func marshalAt(t *testing.T, base string, path string, message proto.Message) string {
	t.Helper()
	path = filepath.Join(base, path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0777); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	b, err := prototext.Marshal(message)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return path
}
