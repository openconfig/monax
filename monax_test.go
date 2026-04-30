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
	"strings"
	"testing"

	"golang.org/x/net/context"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/proto"

	monaxpb "github.com/openconfig/monax/proto"
)

var (
	componentSortOpt = cmpopts.SortSlices(func(x, y *Component) bool {
		return strings.Compare(x.id, y.id) == -1
	})
	componentDiffOpts = []cmp.Option{
		cmp.Comparer(func(x, y *Component) bool {
			return (x.id == y.id &&
				cmp.Equal(x.depComponentByInterfaceID, y.depComponentByInterfaceID, depDiffOpts...) &&
				cmp.Equal(x.requiredTargetByName, y.requiredTargetByName, depDiffOpts...) &&
				cmp.Equal(x.rdeps, y.rdeps, depDiffOpts...))
		}),
		cmpopts.IgnoreFields(Component{}, "requiredInterfaceByName"),
		cmpopts.EquateEmpty(),
		componentSortOpt,
	}
	depDiffOpts = []cmp.Option{
		cmp.Comparer(func(x, y *Component) bool {
			return x.id == y.id
		}),
		cmpopts.EquateEmpty(),
		componentSortOpt,
	}
)

type fakeRuntime struct {
	Parameters       *monaxpb.RuntimeParameters
	SetParametersErr error
}

func (f *fakeRuntime) Initialize(parameters *monaxpb.RuntimeParameters) error {
	f.Parameters = parameters
	return f.SetParametersErr
}
func (f *fakeRuntime) Handler(kind string) Handler {
	if kind == "nil" {
		return nil
	}
	return new(fakeHandler)
}

type fakeHandler struct{}

func (f *fakeHandler) Initialize(ctx context.Context, component *Component) error {
	return nil
}
func (f *fakeHandler) Start(ctx context.Context, component *Component) error {
	return nil
}
func (f *fakeHandler) Stop(ctx context.Context, component *Component) error {
	return nil
}
func (f *fakeHandler) Status(ctx context.Context, component *Component) error {
	return nil
}
func (f *fakeHandler) Targets(component *Component) Targets {
	return new(fakeTargets)
}

type fakeTargets struct {
	UnimplementedTargets
}

func (f fakeTargets) GRPC(ctx context.Context, serviceName string) (Target, error) {
	return EmptyTarget, nil
}

func TestNew(t *testing.T) {
	// NOT parallel because it overwrites functions for testing.
	// t.Parallel()

	readAbstractSUTErr := errors.New("error")
	readLibraryErr := errors.New("error")
	readRuntimeParametersErr := errors.New("error")
	createRuntimeErr := errors.New("error")
	initializeRuntimeErr := errors.New("error")
	processLibraryErr := errors.New("error")
	processAbstractSUTErr := errors.New("error")
	initializeComponentsErr := errors.New("error")

	myFakeRuntime := new(fakeRuntime)
	newMyFakeRuntimeFn := func() (Runtime, error) { return myFakeRuntime, nil }
	tests := map[string]struct {
		config                      *Config
		newRuntimeFn                NewRuntimeFn
		wantRuntime                 Runtime
		wantComponents              []*Component
		wantComponentsByInterfaceID map[interfaceID]*Component
		readAbstractSUTErr          error
		readLibraryErr              error
		readRuntimeParametersErr    error
		processLibraryErr           error
		processAbstractSUTErr       error
		initializeComponentsErr     error
		wantErr                     error
	}{
		"no config": {
			newRuntimeFn: newMyFakeRuntimeFn,
			wantErr:      ErrNoConfig,
		},
		"no new runtime fn": {
			config:  new(Config),
			wantErr: ErrNoNewRuntimeFn,
		},
		"failed read abstract sut": {
			config:             new(Config),
			newRuntimeFn:       newMyFakeRuntimeFn,
			readAbstractSUTErr: readAbstractSUTErr,
			wantErr:            readAbstractSUTErr,
		},
		"failed read library": {
			config:         new(Config),
			newRuntimeFn:   newMyFakeRuntimeFn,
			readLibraryErr: readLibraryErr,
			wantErr:        readLibraryErr,
		},
		"failed read runtime parameters": {
			config:                   new(Config),
			newRuntimeFn:             newMyFakeRuntimeFn,
			readRuntimeParametersErr: readRuntimeParametersErr,
			wantErr:                  readRuntimeParametersErr,
		},
		"failed create runtime": {
			config:       new(Config),
			newRuntimeFn: func() (Runtime, error) { return nil, createRuntimeErr },
			wantErr:      createRuntimeErr,
		},
		"no runtime": {
			config:       new(Config),
			newRuntimeFn: func() (Runtime, error) { return nil, nil },
			wantErr:      ErrNoRuntime,
		},
		"failed initialize runtime": {
			config: new(Config),
			newRuntimeFn: func() (Runtime, error) {
				return &fakeRuntime{
					SetParametersErr: initializeRuntimeErr,
				}, nil
			},
			wantErr: initializeRuntimeErr,
		},
		"failed process abstract sut": {
			config:                new(Config),
			newRuntimeFn:          newMyFakeRuntimeFn,
			processAbstractSUTErr: processAbstractSUTErr,
			wantErr:               processAbstractSUTErr,
		},
		"failed process library": {
			config:            new(Config),
			newRuntimeFn:      newMyFakeRuntimeFn,
			processLibraryErr: processLibraryErr,
			wantErr:           processLibraryErr,
		},
		"failed initialize components": {
			config:       new(Config),
			newRuntimeFn: newMyFakeRuntimeFn,
			wantComponents: []*Component{
				&Component{
					id: "A",
				},
			},
			initializeComponentsErr: initializeComponentsErr,
			wantErr:                 initializeComponentsErr,
		},
		"pass": {
			config:       new(Config),
			newRuntimeFn: newMyFakeRuntimeFn,
			wantRuntime:  myFakeRuntime,
			wantComponents: []*Component{
				&Component{
					id: "A",
				},
			},
			wantComponentsByInterfaceID: map[interfaceID]*Component{
				interfaceID("test.A"): &Component{
					id: "A",
				},
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			// NOT parallel because it overwrites functions for testing.
			// t.Parallel()

			readAbstractSUTFn = func(config *Config) error {
				return test.readAbstractSUTErr
			}
			readLibraryFn = func(config *Config) error {
				return test.readLibraryErr
			}
			readRuntimeParametersFn = func(config *Config) error {
				return test.readRuntimeParametersErr
			}
			processLibraryFn = func(config *Config, runtime Runtime) (map[interfaceID]*Component, error) {
				return nil, test.processLibraryErr
			}
			processAbstractSUTFn = func(abstractSUT *monaxpb.AbstractSut, componentByProvidedInterfaceID map[interfaceID]*Component) ([]*Component, map[interfaceID]*Component, error) {
				return test.wantComponents, test.wantComponentsByInterfaceID, test.processAbstractSUTErr
			}
			initializeComponentsFn = func(context context.Context, components []*Component) error {
				return test.initializeComponentsErr
			}
			t.Cleanup(func() {
				readAbstractSUTFn = readAbstractSUT
				readLibraryFn = readLibrary
				readRuntimeParametersFn = readRuntimeParameters
				processLibraryFn = processLibrary
				processAbstractSUTFn = processAbstractSUT
				initializeComponentsFn = initializeComponents
			})

			ctx := t.Context()
			sut, err := New(ctx, test.config, test.newRuntimeFn)

			if !errors.Is(err, test.wantErr) {
				t.Errorf("New: error diff: want %v, got %v", test.wantErr, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.wantComponents, sut.components, componentDiffOpts...); diff != "" {
				t.Errorf("New: components diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantComponentsByInterfaceID, sut.componentsByInterfaceID, componentDiffOpts...); diff != "" {
				t.Errorf("New: componentsByInterfaceID diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantRuntime, sut.runtime); diff != "" {
				t.Errorf("New: runtime diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(false, sut.started); diff != "" {
				t.Errorf("New: started diff (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNewWithNilRuntimeFn is a separate test because it does not include the
// overrides to the global functions.
func TestNewWithNilRuntimeFn(t *testing.T) {
	t.Parallel()
	config := Config{
		// These are never actually read if the code is working correctly.
		AbstractSUTPath:       "example/math/abstract_sut.txtpb",
		LibraryPath:           "example/math/kubernetes_library.txtpb",
		RuntimeParametersPath: "example/math/kubernetes_runtime_parameters.txtpb",
	}
	sut, err := New(context.Background(), &config, nil)
	if !errors.Is(err, ErrNoNewRuntimeFn) {
		t.Errorf("New: got %v, %v, want nil, %v", sut, err, ErrNoNewRuntimeFn)
	}
}

func TestProcessLibrary(t *testing.T) {
	t.Parallel()

	interfaceAPB := monaxpb.Interface_builder{
		Name: proto.String("A"),
		Grpc: monaxpb.Grpc_builder{
			ServiceName: proto.String("test.A"),
		}.Build(),
	}.Build()
	interfaceA := newInterface(interfaceAPB)
	interfaceBPB := monaxpb.Interface_builder{
		Name: proto.String("B"),
		Grpc: monaxpb.Grpc_builder{
			ServiceName: proto.String("test.B"),
		}.Build(),
	}.Build()
	interfaceB := newInterface(interfaceBPB)
	interfaceCPB := monaxpb.Interface_builder{
		Name: proto.String("C"),
		Grpc: monaxpb.Grpc_builder{
			ServiceName: proto.String("test.C"),
		}.Build(),
	}.Build()
	interfaceC := newInterface(interfaceCPB)

	tests := map[string]struct {
		components     []*monaxpb.Component
		wantComponents map[interfaceID]*Component
		wantErr        error
	}{
		"duplicate component definitions": {
			components: []*monaxpb.Component{
				monaxpb.Component_builder{
					Id: proto.String("A"),
				}.Build(),
				monaxpb.Component_builder{
					Id: proto.String("A"),
				}.Build(),
			},
			wantErr: ErrDuplicateComponentDefinitions,
		},
		"missing handler": {
			wantErr: ErrMissingHandler,
			components: []*monaxpb.Component{
				monaxpb.Component_builder{
					Id:   proto.String("A"),
					Kind: proto.String("nil"),
				}.Build(),
			},
		},
		"duplicate interface providers": {
			components: []*monaxpb.Component{
				monaxpb.Component_builder{
					Id: proto.String("A1"),
					ProvidedInterfaces: []*monaxpb.Interface{
						interfaceAPB,
					},
				}.Build(),
				monaxpb.Component_builder{
					Id: proto.String("A2"),
					ProvidedInterfaces: []*monaxpb.Interface{
						interfaceAPB,
					},
				}.Build(),
			},
			wantErr: ErrDuplicateInterfaceProviders,
		},
		"duplicate interface definitions": {
			components: []*monaxpb.Component{
				monaxpb.Component_builder{
					Id: proto.String("A"),
					RequiredInterfaces: []*monaxpb.Interface{
						monaxpb.Interface_builder{
							Name: proto.String("A_intf"),
							Grpc: monaxpb.Grpc_builder{
								ServiceName: proto.String("test.A"),
							}.Build(),
						}.Build(),
						monaxpb.Interface_builder{
							Name: proto.String("A_intf"),
							Grpc: monaxpb.Grpc_builder{
								ServiceName: proto.String("test.B"),
							}.Build(),
						}.Build(),
					},
				}.Build(),
			},
			wantErr: ErrDuplicateInterfaceDefinitions,
		},
		"missing required interface": {
			components: []*monaxpb.Component{
				monaxpb.Component_builder{
					Id: proto.String("A"),
					RequiredInterfaces: []*monaxpb.Interface{
						interfaceBPB,
					},
				}.Build(),
			},
			wantErr: ErrMissingRequiredInterface,
		},
		"cyclic dependencies": {
			components: []*monaxpb.Component{
				monaxpb.Component_builder{
					Id: proto.String("A"),
					ProvidedInterfaces: []*monaxpb.Interface{
						interfaceAPB,
					},
					RequiredInterfaces: []*monaxpb.Interface{
						interfaceBPB,
					},
				}.Build(),
				monaxpb.Component_builder{
					Id: proto.String("B"),
					ProvidedInterfaces: []*monaxpb.Interface{
						interfaceBPB,
					},
					RequiredInterfaces: []*monaxpb.Interface{
						interfaceAPB,
					},
				}.Build(),
			},
			wantErr: ErrCyclicDependencies,
		},
		"pass": {
			components: []*monaxpb.Component{
				monaxpb.Component_builder{
					Id: proto.String("A"),
					ProvidedInterfaces: []*monaxpb.Interface{
						interfaceAPB,
					},
				}.Build(),
				monaxpb.Component_builder{
					Id: proto.String("B"),
					ProvidedInterfaces: []*monaxpb.Interface{
						interfaceBPB,
					},
					RequiredInterfaces: []*monaxpb.Interface{
						interfaceAPB,
					},
				}.Build(),
				monaxpb.Component_builder{
					Id: proto.String("C"),
					ProvidedInterfaces: []*monaxpb.Interface{
						interfaceCPB,
					},
					RequiredInterfaces: []*monaxpb.Interface{
						interfaceAPB,
						interfaceBPB,
					},
				}.Build(),
			},
			wantComponents: map[interfaceID]*Component{
				interfaceA.id(): &Component{
					id:                        "A",
					depComponentByInterfaceID: map[interfaceID]*Component{},
					requiredTargetByName:      map[string]Target{},
				},
				interfaceB.id(): &Component{
					id:                        "B",
					depComponentByInterfaceID: map[interfaceID]*Component{interfaceA.id(): &Component{id: "A"}},
					requiredTargetByName:      map[string]Target{},
				},
				interfaceC.id(): &Component{
					id:                        "C",
					depComponentByInterfaceID: map[interfaceID]*Component{interfaceA.id(): &Component{id: "A"}, interfaceB.id(): &Component{id: "B"}},
					requiredTargetByName:      map[string]Target{},
				},
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			library := monaxpb.Library_builder{
				Components: test.components,
			}.Build()
			config := &Config{
				Library: library,
			}

			componentByProvidedInterfaceID, err := processLibrary(config, new(fakeRuntime))

			if !errors.Is(err, test.wantErr) {
				t.Errorf("processLibrary: error diff: want %v, got %v", test.wantErr, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.wantComponents, componentByProvidedInterfaceID, componentDiffOpts...); diff != "" {
				t.Errorf("processLibrary: components diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestProcessAbstractSUT(t *testing.T) {
	t.Parallel()

	interfaceAPB := monaxpb.Interface_builder{
		Grpc: monaxpb.Grpc_builder{
			ServiceName: proto.String("test.A"),
		}.Build(),
	}.Build()
	interfaceA := newInterface(interfaceAPB)
	interfaceBPB := monaxpb.Interface_builder{
		Grpc: monaxpb.Grpc_builder{
			ServiceName: proto.String("test.B"),
		}.Build(),
	}.Build()
	interfaceB := newInterface(interfaceBPB)
	interfaceCPB := monaxpb.Interface_builder{
		Grpc: monaxpb.Grpc_builder{
			ServiceName: proto.String("test.C"),
		}.Build(),
	}.Build()
	interfaceC := newInterface(interfaceCPB)

	tests := map[string]struct {
		requiredInterfaces                     []*monaxpb.Interface
		wantSUTComponents                      []*Component
		wantTopComponentsByProvidedInterfaceID map[interfaceID]*Component
		wantErr                                error
	}{
		"no required interfaces": {
			requiredInterfaces: []*monaxpb.Interface{},
			wantErr:            ErrNoRequiredInterfaces,
		},
		"missing required interface": {
			requiredInterfaces: []*monaxpb.Interface{
				monaxpb.Interface_builder{
					Grpc: monaxpb.Grpc_builder{
						ServiceName: proto.String("test.Missing"),
					}.Build(),
				}.Build(),
			},
			wantErr: ErrMissingRequiredInterface,
		},
		"pass with component": {
			requiredInterfaces: []*monaxpb.Interface{
				interfaceAPB,
			},
			wantSUTComponents: []*Component{
				&Component{
					id:                        "A",
					depComponentByInterfaceID: map[interfaceID]*Component{},
				}},
			wantTopComponentsByProvidedInterfaceID: map[interfaceID]*Component{
				interfaceA.id(): &Component{
					id:                        "A",
					depComponentByInterfaceID: map[interfaceID]*Component{},
				},
			},
		},
		"pass with component with direct dependency": {
			requiredInterfaces: []*monaxpb.Interface{
				interfaceBPB,
			},
			wantSUTComponents: []*Component{
				&Component{
					id:                        "A",
					depComponentByInterfaceID: map[interfaceID]*Component{},
					rdeps: []*Component{
						&Component{
							id: "B",
						},
					},
				},
				&Component{
					id: "B",
					depComponentByInterfaceID: map[interfaceID]*Component{
						interfaceA.id(): &Component{
							id: "A",
						},
					},
				},
			},
			wantTopComponentsByProvidedInterfaceID: map[interfaceID]*Component{
				interfaceB.id(): &Component{
					id: "B",
					depComponentByInterfaceID: map[interfaceID]*Component{
						interfaceA.id(): &Component{
							id: "A",
						},
					},
				},
			},
		},
		"pass with component with indirect dependency": {
			requiredInterfaces: []*monaxpb.Interface{
				interfaceCPB,
			},
			wantSUTComponents: []*Component{
				&Component{
					id:                        "A",
					depComponentByInterfaceID: map[interfaceID]*Component{},
					rdeps: []*Component{
						&Component{
							id: "B",
						},
						&Component{
							id: "C",
						},
					},
				},
				&Component{
					id: "B",
					depComponentByInterfaceID: map[interfaceID]*Component{
						interfaceA.id(): &Component{
							id: "A",
						},
					},
					rdeps: []*Component{
						&Component{
							id: "C",
						},
					},
				},
				&Component{
					id: "C",
					depComponentByInterfaceID: map[interfaceID]*Component{
						interfaceA.id(): &Component{
							id: "A",
						},
						interfaceB.id(): &Component{
							id: "B",
						},
					},
				},
			},
			wantTopComponentsByProvidedInterfaceID: map[interfaceID]*Component{
				interfaceC.id(): &Component{
					id: "C",
					depComponentByInterfaceID: map[interfaceID]*Component{
						interfaceA.id(): &Component{
							id: "A",
						},
						interfaceB.id(): &Component{
							id: "B",
						},
					},
				},
			},
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// +---+
			// | C | ------+
			// +---+       |
			//   |         |
			//   |         |
			//   v         v
			// +---+     +---+
			// | B | --> | A |
			// +---+     +---+
			// input components as ProcessAbstractSUT would receive as input from ProcessLibrary
			var inputComponentA, inputComponentB, inputComponentC *Component
			inputComponentA = &Component{
				id:                        "A",
				depComponentByInterfaceID: map[interfaceID]*Component{},
			}
			inputComponentB = &Component{
				id:                        "B",
				depComponentByInterfaceID: map[interfaceID]*Component{interfaceA.id(): inputComponentA},
			}
			inputComponentC = &Component{
				id:                        "C",
				depComponentByInterfaceID: map[interfaceID]*Component{interfaceA.id(): inputComponentA, interfaceB.id(): inputComponentB},
			}
			allComponentsByProvidedInterfaceID := map[interfaceID]*Component{
				newInterface(interfaceAPB).id(): inputComponentA,
				newInterface(interfaceBPB).id(): inputComponentB,
				newInterface(interfaceCPB).id(): inputComponentC,
			}

			abstractSUT := monaxpb.AbstractSut_builder{
				RequiredInterfaces: test.requiredInterfaces,
			}.Build()

			sutComponents, topComponentsByProvidedInterfaceID, err := processAbstractSUT(abstractSUT, allComponentsByProvidedInterfaceID)

			if !errors.Is(err, test.wantErr) {
				t.Errorf("processAbstractSUT: error diff: want %v, got %v", test.wantErr, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.wantSUTComponents, sutComponents, componentDiffOpts...); diff != "" {
				t.Errorf("processAbstractSUT: components diff (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantTopComponentsByProvidedInterfaceID, topComponentsByProvidedInterfaceID, componentDiffOpts...); diff != "" {
				t.Errorf("processAbstractSUT: requiredInterfacesToComponents diff (-want +got):\n%s", diff)
			}
		})
	}
}
