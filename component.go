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
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"golang.org/x/net/context"

	log "github.com/golang/glog"
	"golang.org/x/exp/maps"

	monaxpb "github.com/openconfig/monax/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

// ComponentIDRegex is the regex for a valid component ID.
// The ID must start with a letter, and may only contain letters, numbers,
// dashes, and underscores.
// Whitespace is not allowed due to how the CLIs and other tools parse args with
// spaces.
const ComponentIDRegex = `^[a-zA-Z][a-zA-Z0-9-_]*$`

var (
	// ErrDuplicateInterfaceDefinitions indicates the library is invalid because
	// it contains a component with duplicate interface definitions.
	ErrDuplicateInterfaceDefinitions = errors.New("duplicate interface definitions")

	// ErrProvidedInterface indicates that an error was received from the
	// component handler when trying to retrieve a provided interface.
	ErrProvidedInterface = errors.New("retrieving provided interface")

	// ErrUnknownRequiredInterfaceName indicates that the required interface name
	// could not be found in the component's required interfaces.
	ErrUnknownRequiredInterfaceName = errors.New("unknown required interface name")

	// ErrInvalidComponentID indicates that the component ID in the library is
	// invalid.
	ErrInvalidComponentID = errors.New("invalid component ID")
)

// Component is an element in the SUT that satisfies direct or transitive
// dependencies of the abstract SUT.
type Component struct {
	requiredInterfaceByName   map[string]componentInterface
	id                        string
	parameters                *anypb.Any
	baseDir                   string                     // directory where relative paths in this component are relative to
	depComponentByInterfaceID map[interfaceID]*Component // components that we depend on
	requiredTargetByName      map[string]Target          // targets that we depend on
	rdeps                     []*Component               // components that depend on us
	handler                   Handler
	done                      chan bool
}

func (c *Component) String() string {
	return c.id
}

// ID returns the component id.
func (c *Component) ID() string {
	return c.id
}

// Parameters returns the component parameters.
func (c *Component) Parameters() *anypb.Any {
	return c.parameters
}

// ResolvePath resolves a textproto path using the relative path information
// injected into the component when the library is built.
func (c *Component) ResolvePath(path string) string {
	path = os.ExpandEnv(path)

	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(c.baseDir, path)
}

// providedTarget returns the target corresponding to the given interface
// specified by the given interface proto if this component provides it.
func (c *Component) providedTarget(ctx context.Context, providedInterface componentInterface) (Target, error) {
	var target Target
	var err error
	switch v := providedInterface.(type) {
	case *dhcpInterface:
		target, err = c.handler.Targets(c).DHCP(ctx, v.serviceName)
	case *grpcInterface:
		target, err = c.handler.Targets(c).GRPC(ctx, v.serviceName)
	case *httpInterface:
		target, err = c.handler.Targets(c).HTTP(ctx, v.serviceName)
	case *httpsInterface:
		target, err = c.handler.Targets(c).HTTPS(ctx, v.serviceName)
	default:
		log.FatalContextf(ctx, "Unexpected interface type: %v", v)
	}
	if err != nil {
		err = fmt.Errorf("%w: %v", ErrProvidedInterface, err)
	}
	return target, err
}

// RequiredTarget returns the target for the given required interface name.
// Required interface names are specified by the user in the component proto.
func (c *Component) RequiredTarget(name string) (Target, error) {
	if target, ok := c.requiredTargetByName[name]; ok {
		return target, nil
	}
	return EmptyTarget, fmt.Errorf("%w: required interface name %s in component %v", ErrUnknownRequiredInterfaceName, name, c)
}

func newComponent(pb *monaxpb.Component, baseDir string) (*Component, error) {
	requiredInterfaceByName := make(map[string]componentInterface)
	for _, requiredInterfacePB := range pb.GetRequiredInterfaces() {
		if _, ok := requiredInterfaceByName[requiredInterfacePB.GetName()]; ok {
			return nil, fmt.Errorf("%w: component %s: required interface %s", ErrDuplicateInterfaceDefinitions, pb.GetId(), requiredInterfacePB.GetName())
		}
		requiredInterfaceByName[requiredInterfacePB.GetName()] = newInterface(requiredInterfacePB)
	}

	if !regexp.MustCompile(ComponentIDRegex).MatchString(pb.GetId()) {
		return nil, fmt.Errorf(
			"%w: component ID %q: must start with a letter, and may only contain "+
				"letters, numbers, dashes, and underscores",
			ErrInvalidComponentID,
			pb.GetId(),
		)
	}

	return &Component{
		id:                        pb.GetId(),
		parameters:                pb.GetParameters(),
		baseDir:                   baseDir,
		requiredInterfaceByName:   requiredInterfaceByName,
		depComponentByInterfaceID: make(map[interfaceID]*Component),
		requiredTargetByName:      make(map[string]Target),
	}, nil
}

func (c *Component) initialize(ctx context.Context) error {
	log.InfoContextf(ctx, "Initializing component %v", c)
	if err := c.handler.Initialize(ctx, c); err != nil {
		log.ErrorContextf(ctx, "Initializing component %v failed: %v", c, err)
		return err
	}
	log.InfoContextf(ctx, "Initialized component %v", c)
	return nil
}

func (c *Component) start(ctx context.Context) error {
	log.InfoContextf(ctx, "Starting component %v", c)

	// Populate the target of each required interface.
	for name, requiredInterface := range c.requiredInterfaceByName {
		dep := c.depComponentByInterfaceID[requiredInterface.id()]
		target, err := dep.providedTarget(ctx, requiredInterface)
		if err != nil {
			return err
		}
		c.requiredTargetByName[name] = target
	}

	if err := c.handler.Start(ctx, c); err != nil {
		log.ErrorContextf(ctx, "Starting component %v failed: %v", c, err)
		return err
	}

	// TODO(team): check the status of each provided interface before we finish here.
	log.InfoContextf(ctx, "Started component %v", c)
	return nil
}

func (c *Component) stop(ctx context.Context) error {
	log.InfoContextf(ctx, "Stopping component %v", c)
	if err := c.handler.Stop(ctx, c); err != nil {
		log.ErrorContextf(ctx, "Stopping component %v failed: %v", c, err)
		return err
	}
	log.InfoContextf(ctx, "Stopped component %v", c)
	return nil
}

func (c *Component) status(ctx context.Context) error {
	return c.handler.Status(ctx, c)
}

func channelize(components []*Component) <-chan bool {
	var wg sync.WaitGroup
	out := make(chan bool)

	output := func(c <-chan bool) {
		for range c {
		}
		wg.Done()
	}
	wg.Add(len(components))
	for _, component := range components {
		go output(component.done)
	}

	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

type direction bool

const forward = direction(true)  // wait for things that we depend on
const reverse = direction(false) // wait for things that depend on us

func walk(components []*Component, direction direction, fn func(component *Component) error) error {
	// Set up a channel for each component to signal to its dependants (or
	// dependencies, for the reverse case) when it is done.
	for _, component := range components {
		component.done = make(chan bool)
	}

	var mu sync.Mutex
	se := newSUTError()

	// Do the operation on each component.
	for _, component := range components {
		component := component
		go func() {
			// When we're done, we'll signal to the dependants (or dependencies, for
			// the reverse case) of this component that it is finished.
			defer close(component.done)

			// Wait until the dependencies (or dependants, for the reverse case) of
			// this component have finished.
			deps := maps.Values(component.depComponentByInterfaceID)
			if direction == reverse {
				deps = component.rdeps
			}
			for range channelize(deps) {
			}

			// Do the operation, collecting any error.
			if err := fn(component); err != nil {
				mu.Lock()
				defer mu.Unlock()
				se.ErrorByComponent[component] = err
			}
		}()
	}

	// Wait for all of the components to finish.
	for range channelize(components) {
	}

	return se.orNil()
}
