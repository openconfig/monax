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
	"fmt"
	"strings"
	"sync"

	"golang.org/x/net/context"

	log "github.com/golang/glog"
)

var (
	// ErrAlreadyStarted indicates the SUT cannot perform an operation because it
	// is already started.
	ErrAlreadyStarted = errors.New("already started")

	// ErrNotStarted indicates the SUT cannot perform an operation because it is
	// not started.
	ErrNotStarted = errors.New("not started")
)

// SUT is a system under test.
type SUT struct {
	components              []*Component
	componentsByInterfaceID map[interfaceID]*Component
	runtime                 Runtime
	started                 bool
	mu                      sync.Mutex
	targets                 *SUTTargets
	interfaces              *SUTInterfaces
}

func (s *SUT) String() string {
	out := make([]string, 0, len(s.components))
	for _, component := range s.components {
		out = append(out, fmt.Sprintf("%v→%v→%v", component.rdeps, component, component.depComponentByInterfaceID))
	}
	return strings.Join(out, ", ")
}

// Started returns true if the SUT is started.
func (s *SUT) Started() bool {
	return s.started
}

func initializeComponents(ctx context.Context, components []*Component) error {
	return walk(components, forward, func(component *Component) error {
		return component.initialize(ctx)
	})
}

func (s *SUT) walk(direction direction, fn func(component *Component) error) error {
	return walk(s.components, direction, fn)
}

// Start starts the components in the SUT.  If component A depends on an
// interface provided by component B, A will be started only after B has
// started.
//
// Returns ErrAlreadyStarted if the SUT is already started.  Otherwise, returns
// a SUTError if any component fails to start.
//
// Caller should stop the SUT when done with it by calling [Stop].
//
//	if err := sut.Start(ctx); err != nil {
//		// handle error
//	}
//	defer func() {
//		if err := sut.Stop(ctx); err != nil {
//			// handle error
//		}
//	}
//
// If the error will be handled by exiting the program, including by log.Exit
// or log.Fatal, then the caller should stop the SUT prior to exiting.
//
//	if err := sut.Start(ctx); err != nil {
//		err = errors.Join(err, sut.Stop(ctx))
//		log.ExitContext(ctx, err)
//	}
//	defer func() {
//		if err := sut.Stop(ctx); err != nil {
//			// handle error
//		}
//	}
func (s *SUT) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return ErrAlreadyStarted
	}

	log.InfoContextf(ctx, "Starting SUT")
	err := s.walk(forward, func(component *Component) error {
		return component.start(ctx)
	})
	if err == nil {
		log.InfoContextf(ctx, "Started SUT")
	} else {
		log.ErrorContextf(ctx, "Started SUT with errors")
	}

	s.started = true
	return err
}

// Stop stops the components in the SUT.  If component A depends on an interface
// provided by component B, B will be stopped only after A has stopped.
//
// If Stop fails, the SUT may need to be stopped manually.  See [Start] for
// examples for calling Stop.
//
// Returns ErrNotStarted if the SUT is not started.  Otherwise, returns a
// SUTError if any component fails to stop.
func (s *SUT) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return ErrNotStarted
	}

	s.interfaces.closeCachedInterfaces()

	log.InfoContextf(ctx, "Stopping SUT")
	err := s.walk(reverse, func(component *Component) error {
		return component.stop(ctx)
	})
	if err == nil {
		log.InfoContextf(ctx, "Stopped SUT")
	} else {
		log.ErrorContextf(ctx, "Stopped SUT with errors; the SUT may need to be stopped manually")
	}

	s.started = false
	return err
}

// Status gets the status of the components in the SUT.
//
// Returns ErrNotStarted if the SUT is not started.  Otherwise, returns a
// SUTError if any component has errors.
func (s *SUT) Status(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return ErrNotStarted
	}

	return s.walk(forward, func(component *Component) error {
		return component.status(ctx)
	})
}

// Targets returns a handle to the targets API.
func (s *SUT) Targets() *SUTTargets {
	return s.targets
}

// Interfaces returns a handle to the interfaces API.
func (s *SUT) Interfaces() *SUTInterfaces {
	return s.interfaces
}

// SUTError is an aggregation of component errors.
type SUTError struct {
	ErrorByComponent map[*Component]error
}

func newSUTError() *SUTError {
	return &SUTError{
		ErrorByComponent: make(map[*Component]error),
	}
}

func (err *SUTError) Error() string {
	if err.empty() {
		return ""
	}
	errs := make([]string, 0, len(err.ErrorByComponent))
	for component, err := range err.ErrorByComponent {
		errs = append(errs, fmt.Sprintf("component %v: %v", component, err))
	}
	return fmt.Sprintf("components have errors: %v", strings.Join(errs, ", "))
}

// PrettyPrint returns a human readable string representation of the SUTError.
func (err *SUTError) PrettyPrint() string {
	if err.empty() {
		return ""
	}
	errs := make([]string, 0, len(err.ErrorByComponent))
	for component, err := range err.ErrorByComponent {
		errs = append(errs, fmt.Sprintf("component %v: %v", component, err))
	}
	return fmt.Sprintf("components have errors:\n\t%v", strings.Join(errs, "\n\t"))
}

func (err *SUTError) empty() bool {
	return err == nil || len(err.ErrorByComponent) == 0
}

func (err *SUTError) orNil() error {
	if err.empty() {
		return nil
	}
	return err
}
