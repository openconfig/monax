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

type dhcpInterface struct {
	serviceName string
}

func (i dhcpInterface) String() string {
	return string(i.id())
}

func (i dhcpInterface) id() interfaceID {
	return interfaceID(fmt.Sprintf("DHCP %s", i.serviceName))
}

// DHCP returns the target of the DHCP service with the given serviceName from
// the SUT.
func (t *SUTTargets) DHCP(ctx context.Context, serviceName string) (string, error) {
	if !t.sut.started {
		return "", ErrNotStarted
	}
	target, err := t.findTarget(ctx, &dhcpInterface{
		serviceName: serviceName,
	})
	return string(target), err
}

// DHCP returns the URL of the DHCP service with the given serviceName from the
// SUT.
func (i *SUTInterfaces) DHCP(ctx context.Context, serviceName string) (string, error) {
	return i.sut.Targets().DHCP(ctx, serviceName)
}

// DHCPSelector returns a function that matches a DHCP interface with the given
// service name.
func DHCPSelector(serviceName string) func(*monaxpb.Interface) bool {
	return func(intf *monaxpb.Interface) bool {
		return intf.GetDhcp().GetServiceName() == serviceName
	}
}
