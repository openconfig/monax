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

// Package kubernetesruntime is the Monax runtime for Kubernetes components.
package kubernetesruntime

import (
	"errors"
	"fmt"

	"github.com/openconfig/monax"

	monaxpb "github.com/openconfig/monax/proto"
	runtimepb "github.com/openconfig/monax/runtime/kubernetesruntime/proto"
)

var (
	// ErrInvalidRuntimeParameters indicates the runtime parameters given
	// could not be unmarshaled.
	ErrInvalidRuntimeParameters = errors.New("invalid runtime parameters")

	// ErrNoRuntimeParameters indicates no runtime parameters were set.
	ErrNoRuntimeParameters = errors.New("no runtime parameters")
)

type handler interface {
	monax.Handler
	setParameters(*runtimepb.KubernetesRuntimeParameters) error
}

type runtime struct {
	handlers   map[string]handler
	parameters *runtimepb.KubernetesRuntimeParameters
}

// New makes a new Kubernetes runtime.
func New() (monax.Runtime, error) {
	return &runtime{
		handlers: map[string]handler{
			"kubernetes": newKubernetesHandler(),
		},
	}, nil
}

func (r *runtime) Initialize(parameters *monaxpb.RuntimeParameters) error {
	if parameters.GetParameters() == nil {
		return ErrNoRuntimeParameters
	}
	r.parameters = new(runtimepb.KubernetesRuntimeParameters)
	if err := parameters.GetParameters().UnmarshalTo(r.parameters); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidRuntimeParameters, err)
	}

	for kind, handler := range r.handlers {
		if err := handler.setParameters(r.parameters); err != nil {
			return fmt.Errorf("setting parameters for component %v: %w", kind, err)
		}
	}

	return nil
}

func (r *runtime) Handler(kind string) monax.Handler {
	return r.handlers[kind]
}
