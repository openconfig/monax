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
	"sync"
	"time"

	log "github.com/golang/glog"
	"google.golang.org/grpc"

	monaxpb "github.com/openconfig/monax/proto"
)

const (
	dialTimeout = 30 * time.Second
)

var (
	// ErrRequestedInterfaceNotProvided indicates that the requested interface is
	// not provided by the SUT because it was not specified as a required
	// interface of the abstract SUT.
	ErrRequestedInterfaceNotProvided = errors.New("requested interface not provided by the SUT")
)

// interfaceID is the id for an interface.
type interfaceID string

// componentInterface is a typed communication boundary between components.  It
// corresponds to the Interface protobuf message, but "interface" cannot be used
// here because it is a reserved word.
type componentInterface interface {
	fmt.Stringer
	id() interfaceID
}

func newInterface(pb *monaxpb.Interface) componentInterface {
	switch pb.WhichType() {
	case monaxpb.Interface_Dhcp_case:
		return &dhcpInterface{
			serviceName: pb.GetDhcp().GetServiceName(),
		}
	case monaxpb.Interface_Grpc_case:
		return &grpcInterface{
			serviceName: pb.GetGrpc().GetServiceName(),
		}
	case monaxpb.Interface_Http_case:
		return &httpInterface{
			serviceName: pb.GetHttp().GetServiceName(),
		}
	case monaxpb.Interface_Https_case:
		return &httpsInterface{
			serviceName: pb.GetHttps().GetServiceName(),
		}
	default:
		log.Fatalf("Unexpected interface type: %v", pb)
		panic("unreachable")
	}
}

// SUTInterfaces implements methods for retrieving different kinds of
// interfaces from the SUT.
type SUTInterfaces struct {
	sut                   *SUT
	grpcConnByServiceName map[string]*grpc.ClientConn
	mu                    sync.Mutex
}

func newSUTInterfaces(sut *SUT) *SUTInterfaces {
	return &SUTInterfaces{
		sut:                   sut,
		grpcConnByServiceName: make(map[string]*grpc.ClientConn),
	}
}

func (i *SUTInterfaces) findComponent(intf componentInterface) (*Component, error) {
	component, ok := i.sut.componentsByInterfaceID[intf.id()]
	if !ok {
		return nil, fmt.Errorf("%w: interface %v", ErrRequestedInterfaceNotProvided, intf)
	}
	return component, nil
}

func (i *SUTInterfaces) closeCachedInterfaces() {
	for _, conn := range i.grpcConnByServiceName {
		conn.Close()
	}
	i.grpcConnByServiceName = make(map[string]*grpc.ClientConn)
}
