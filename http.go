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
	"fmt"

	"golang.org/x/net/context"

	monaxpb "github.com/openconfig/monax/proto"
)

type httpInterface struct {
	serviceName string
}

func (i httpInterface) String() string {
	return string(i.id())
}

func (i httpInterface) id() interfaceID {
	return interfaceID(fmt.Sprintf("HTTP %s", i.serviceName))
}

type httpsInterface struct {
	serviceName string
}

func (i httpsInterface) String() string {
	return string(i.id())
}

func (i httpsInterface) id() interfaceID {
	return interfaceID(fmt.Sprintf("HTTPS %s", i.serviceName))
}

// HTTP returns the target of the HTTP service with the given serviceName from
// the SUT.
func (t *SUTTargets) HTTP(ctx context.Context, serviceName string) (string, error) {
	return t.httpTarget(ctx, serviceName, &httpInterface{
		serviceName: serviceName,
	})
}

// HTTPS returns the target of the HTTPS service with the given serviceName from
// the SUT.
func (t *SUTTargets) HTTPS(ctx context.Context, serviceName string) (string, error) {
	return t.httpTarget(ctx, serviceName, &httpsInterface{
		serviceName: serviceName,
	})
}

func (t *SUTTargets) httpTarget(ctx context.Context, serviceName string, providedInterface componentInterface) (string, error) {
	if !t.sut.started {
		return "", ErrNotStarted
	}
	target, err := t.findTarget(ctx, providedInterface)
	if err != nil {
		return "", err
	}
	return string(target), nil
}

// HTTP returns the URL of the HTTP service with the given serviceName from the
// SUT.
func (i *SUTInterfaces) HTTP(ctx context.Context, serviceName string) (string, error) {
	return i.sut.Targets().HTTP(ctx, serviceName)
}

// HTTPS returns the URL of the HTTPS service with the given serviceName from
// the SUT.
func (i *SUTInterfaces) HTTPS(ctx context.Context, serviceName string) (string, error) {
	return i.sut.Targets().HTTPS(ctx, serviceName)
}

// HTTPSelector returns a function that matches an HTTP interface with the given
// service name.
func HTTPSelector(serviceName string) func(*monaxpb.Interface) bool {
	return func(intf *monaxpb.Interface) bool {
		return intf.GetHttp().GetServiceName() == serviceName
	}
}

// HTTPSSelector returns a function that matches an HTTPS interface with the
// given service name.
func HTTPSSelector(serviceName string) func(*monaxpb.Interface) bool {
	return func(intf *monaxpb.Interface) bool {
		return intf.GetHttps().GetServiceName() == serviceName
	}
}
