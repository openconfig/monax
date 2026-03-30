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

// Package monax represents the public Monax API.
package monax

import (
	"errors"
	"fmt"

	"golang.org/x/net/context"

	log "github.com/golang/glog"
	"golang.org/x/exp/maps"

	monaxpb "github.com/openconfig/monax/proto"
)

var (
	// ErrCyclicDependencies indicates the library is invalid because it contains
	// cyclic dependencies.
	ErrCyclicDependencies = errors.New("cyclic dependencies")

	// ErrDuplicateComponentDefinitions indicates the library is invalid because
	// it contains duplicate definitions of a component.
	ErrDuplicateComponentDefinitions = errors.New("duplicate component definitions")

	// ErrDuplicateInterfaceProviders indicates the library is invalid because it
	// contains duplicate providers of an interface.
	ErrDuplicateInterfaceProviders = errors.New("duplicate interface providers")

	// ErrMissingHandler indicates the library is invalid because it contains
	// components with no handler in the runtime.
	ErrMissingHandler = errors.New("missing handler")

	// ErrMissingRequiredInterface indicates the abstract SUT or library are
	// invalid because an interface required by the abstract SUT or a component in
	// the library is not provided by the library.
	ErrMissingRequiredInterface = errors.New("missing required interface")

	// ErrNoConfig indicates a Config was not set.
	ErrNoConfig = errors.New("no config")

	// ErrNoRequiredInterfaces indicates the abstract SUT is invalid because it
	// specifies no required interfaces.
	ErrNoRequiredInterfaces = errors.New("no required interfaces")

	// ErrNoNewRuntimeFn indicates a new runtime function was not set.
	ErrNoNewRuntimeFn = errors.New("no new runtime function")

	// ErrNoRuntime indicates a runtime was not returned by the new runtime
	// function.
	ErrNoRuntime = errors.New("no runtime")
)

var (
	processLibraryFn       = processLibrary
	processAbstractSUTFn   = processAbstractSUT
	initializeComponentsFn = initializeComponents
)

// New makes a new SUT from a Config and Runtime.
func New(ctx context.Context, config *Config, newRuntimeFn NewRuntimeFn) (*SUT, error) {
	if config == nil {
		return nil, ErrNoConfig
	}
	if newRuntimeFn == nil {
		return nil, ErrNoNewRuntimeFn
	}
	if err := readConfig(config); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	runtime, err := newRuntimeFn()
	if err != nil {
		return nil, fmt.Errorf("create runtime: %w", err)
	}
	if runtime == nil {
		return nil, ErrNoRuntime
	}
	if err := runtime.Initialize(config.RuntimeParameters); err != nil {
		return nil, fmt.Errorf("initialize runtime: %w", err)
	}

	componentByProvidedInterfaceID, err := processLibraryFn(config, runtime)
	if err != nil {
		return nil, fmt.Errorf("process library: %w", err)
	}
	components, componentsByInterfaceID, err := processAbstractSUTFn(config.AbstractSUT, componentByProvidedInterfaceID)
	if err != nil {
		return nil, fmt.Errorf("process abstract SUT: %w", err)
	}
	if err := initializeComponentsFn(ctx, components); err != nil {
		return nil, fmt.Errorf("initialize components: %w", err)
	}

	sut := &SUT{
		components:              components,
		componentsByInterfaceID: componentsByInterfaceID,
		runtime:                 runtime,
	}
	sut.targets = newSUTTargets(sut)
	sut.interfaces = newSUTInterfaces(sut)

	return sut, nil
}

// processLibrary produces a mapping of components by provided interface IDs.
// The library is validated based on the following criteria:
//
//  1. No duplicate ids.  It is an error if component A and another component A
//     both exist in the library.
//  2. No unhandled components.  It is an error if component A is of type T but
//     the runtime does not have a handler for the same type T.
//  3. No duplicate provided interfaces.  It is an error if component A and
//     component B both provide the same interface I.
//  4. No unsatisfied required interfaces.  It is an error if component A
//     requires interface I but no component provides the same interface I.
//  5. No cyclic required interfaces.  It is an error if component A directly
//     or indirectly depends on component B but component B also directly or
//     indirectly depends on component A.
//
// These conditions are treated as errors even if the SUT could be constructed
// to avoid them.  For example, if a problem exists in component A, but the
// SUT will only use component B, it is still an error.  Although ignoring
// errors in unused components may make things easier for the user in some
// cases, it will inevitably lead to confusion when the same library is valid
// for one test but invalid for another.
func processLibrary(config *Config, runtime Runtime) (map[interfaceID]*Component, error) {
	// Check that components are unique and have handlers (1, 2) and that provided
	// interfaces are unique (3).
	componentByID := make(map[string]*Component)
	componentByProvidedInterfaceID := make(map[interfaceID]*Component)
	for _, componentPB := range config.Library.GetComponents() {
		component, err := newComponent(componentPB, config.componentBaseDirByID[componentPB.GetId()])
		if err != nil {
			return nil, err
		}
		if _, ok := componentByID[component.id]; ok {
			return nil, fmt.Errorf("%w: component %v", ErrDuplicateComponentDefinitions, component)
		}
		component.handler = runtime.Handler(componentPB.GetKind())
		if component.handler == nil {
			return nil, fmt.Errorf("%w: component %v with kind %v", ErrMissingHandler, component, componentPB.GetKind())
		}
		componentByID[component.id] = component

		for _, providedInterfacePB := range componentPB.GetProvidedInterfaces() {
			providedInterface := newInterface(providedInterfacePB)
			if component, ok := componentByProvidedInterfaceID[providedInterface.id()]; ok {
				return nil, fmt.Errorf("%w: interface %v: component %v and component %v", ErrDuplicateInterfaceProviders, providedInterface, component, componentPB.GetId())
			}
			componentByProvidedInterfaceID[providedInterface.id()] = component
		}
	}

	// Check that required interfaces are satisfied (4).
	//
	// Dependencies are added here, but reverse dependencies are not.  This is due
	// to how starting and stopping the SUT works.
	//
	// During startup, dependencies matter.  If this component is not a transitive
	// dependency of the abstract SUT, the SUT won't try to start it and nothing
	// else will wait for it to start because it won't be in the dependency chain.
	// It'll effectively be ignored, and its dependencies won't matter (although
	// they may still end up in the SUT due to other components that are
	// included).  Tracking dependencies here is harmless.
	//
	// During shutdown, reverse dependencies matter.  If we were to add reverse
	// dependencies here and one of those reverse dependencies were to be included
	// in the SUT, then stopping the SUT would fail.  This is because each
	// component waits for the components that depend on it (i.e., its reverse
	// dependencies) to stop before it stops itself.  If we make a connection
	// between this component and its reverse dependencies and this component gets
	// dropped from the SUT, trying to stop the SUT will hang.  Instead, we have
	// to add reverse dependencies after we know exactly which components are
	// required.
	for _, componentPB := range config.Library.GetComponents() {
		component := componentByID[componentPB.GetId()]
		depComponentByRequiredInterfaceID := make(map[interfaceID]*Component)
		for _, requiredInterfacePB := range componentPB.GetRequiredInterfaces() {
			requiredInterfaceID := newInterface(requiredInterfacePB).id()
			dep, ok := componentByProvidedInterfaceID[requiredInterfaceID]
			if !ok {
				return nil, fmt.Errorf("%w: component %v: interface %v", ErrMissingRequiredInterface, component, requiredInterfaceID)
			}
			depComponentByRequiredInterfaceID[requiredInterfaceID] = dep
		}
		component.depComponentByInterfaceID = depComponentByRequiredInterfaceID
		component.requiredTargetByName = make(map[string]Target)
	}

	// Check that component dependencies are acyclic (5).
	type status int
	const (
		notChecked status = iota
		beingChecked
		alreadyChecked
	)
	statusByComponent := make(map[*Component]status)
	var hasCycle func(component *Component) bool
	hasCycle = func(component *Component) bool {
		switch status := statusByComponent[component]; status {
		case notChecked:
			statusByComponent[component] = beingChecked
			for _, dep := range component.depComponentByInterfaceID {
				if hasCycle(dep) {
					return true
				}
			}
			statusByComponent[component] = alreadyChecked
			return false
		case beingChecked:
			return true
		case alreadyChecked:
			return false
		default:
			log.Fatalf("Unexpected status for component %v: %v", component, status)
			panic("unreachable")
		}
	}
	for _, component := range componentByID {
		if hasCycle(component) {
			return nil, fmt.Errorf("%w: component %v", ErrCyclicDependencies, component)
		}
	}

	return componentByProvidedInterfaceID, nil
}

// processAbstractSUT produces the list of components needed to satisfy the
// requirements of the abstract SUT.  The abstract SUT is validated based on the
// following criteria:
//
//  1. At least one required interface.  It is an error if the abstract SUT does
//     not require any interfaces.
//  2. No unsatisfied required interfaces.  It is an error if the abstract SUT
//     requires interface I but no component in the library provides the same
//     interface I.
func processAbstractSUT(abstractSUT *monaxpb.AbstractSut, componentByProvidedInterfaceID map[interfaceID]*Component) ([]*Component, map[interfaceID]*Component, error) {
	// Check that at least one interface is required (1).
	if len(abstractSUT.GetRequiredInterfaces()) == 0 {
		return nil, nil, ErrNoRequiredInterfaces
	}

	// Check that required interfaces are satisfied (2).
	for _, requiredInterfacePB := range abstractSUT.GetRequiredInterfaces() {
		requiredInterface := newInterface(requiredInterfacePB)
		_, ok := componentByProvidedInterfaceID[requiredInterface.id()]
		if !ok {
			return nil, nil, fmt.Errorf("%w: interface %v", ErrMissingRequiredInterface, requiredInterface)
		}
	}

	includedByComponent := make(map[*Component]bool)
	var addDeps func(component *Component)
	addDeps = func(component *Component) {
		if includedByComponent[component] {
			return
		}
		for _, dep := range component.depComponentByInterfaceID {
			addDeps(dep)

			// Now that we know which components are included in the SUT, we can make
			// connections between components and their reverse dependencies.
			dep.rdeps = append(dep.rdeps, component)
		}
		includedByComponent[component] = true
	}

	componentByRequiredInterfaceID := make(map[interfaceID]*Component)
	for _, requiredInterfacePB := range abstractSUT.GetRequiredInterfaces() {
		requiredInterface := newInterface(requiredInterfacePB)
		component, ok := componentByProvidedInterfaceID[requiredInterface.id()]
		if !ok {
			log.Fatalf("Unexpected missing interface: interface %v", requiredInterface)
		}

		componentByRequiredInterfaceID[requiredInterface.id()] = component
		addDeps(component)
	}

	return maps.Keys(includedByComponent), componentByRequiredInterfaceID, nil
}
