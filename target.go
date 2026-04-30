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

	"golang.org/x/net/context"
)

var (
	// ErrTargetNotImplemented indicates that a handler has not implemented the
	// requested target retriever type.
	ErrTargetNotImplemented = errors.New("target not implemented")
)

// Target represents the target for an interface.
type Target string

// EmptyTarget represents an empty target.
const EmptyTarget = Target("")

// SUTTargets implements methods for retrieving different kinds of targets from
// the SUT.
type SUTTargets struct {
	sut *SUT
}

func newSUTTargets(sut *SUT) *SUTTargets {
	return &SUTTargets{
		sut: sut,
	}
}

// Targets is an interface for retrieving targets of provided interfaces in the
// SUT.
type Targets interface {
	DHCP(ctx context.Context, serviceName string) (Target, error)
	GRPC(ctx context.Context, serviceName string) (Target, error)
	HTTP(ctx context.Context, serviceName string) (Target, error)
	HTTPS(ctx context.Context, serviceName string) (Target, error)
}

// UnimplementedTargets is a struct that implements Targets and returns
// ErrTargetNotImplemented for all target types.  By embedding this struct,
// implementations of Targets can ignore target types that they do not support.
type UnimplementedTargets struct{}

// DHCP returns ErrTargetNotImplemented.
func (UnimplementedTargets) DHCP(ctx context.Context, serviceName string) (Target, error) {
	return EmptyTarget, fmt.Errorf("%w: DHCP %s", ErrTargetNotImplemented, serviceName)
}

// GRPC returns ErrTargetNotImplemented.
func (UnimplementedTargets) GRPC(ctx context.Context, serviceName string) (Target, error) {
	return EmptyTarget, fmt.Errorf("%w: gRPC %s", ErrTargetNotImplemented, serviceName)
}

// HTTP returns ErrTargetNotImplemented.
func (UnimplementedTargets) HTTP(ctx context.Context, serviceName string) (Target, error) {
	return EmptyTarget, fmt.Errorf("%w: HTTP %s", ErrTargetNotImplemented, serviceName)
}

// HTTPS returns ErrTargetNotImplemented.
func (UnimplementedTargets) HTTPS(ctx context.Context, serviceName string) (Target, error) {
	return EmptyTarget, fmt.Errorf("%w: HTTPS %s", ErrTargetNotImplemented, serviceName)
}

func (t *SUTTargets) findTarget(ctx context.Context, providedInterface componentInterface) (Target, error) {
	component, err := t.sut.Interfaces().findComponent(providedInterface)
	if err != nil {
		return EmptyTarget, err
	}
	return component.providedTarget(ctx, providedInterface)
}
