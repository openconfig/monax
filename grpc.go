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

	"github.com/cenkalti/backoff"
	log "github.com/golang/glog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	monaxpb "github.com/openconfig/monax/proto"
	refgrpc "google.golang.org/grpc/reflection/grpc_reflection_v1"
	refpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
)

var (

	// ErrDialGRPCService indicates that the gRPC service could not be dialed.
	ErrDialGRPCService = errors.New("dial gRPC service")

	// ErrVerifyGRPCService indicates that the gRPC service could not be verified.
	ErrVerifyGRPCService = errors.New("verify gRPC service")
)

type grpcInterface struct {
	serviceName string
}

func (i grpcInterface) String() string {
	return string(i.id())
}

func (i grpcInterface) id() interfaceID {
	return interfaceID(fmt.Sprintf("gRPC %s", i.serviceName))
}

// GRPC returns the target of the gRPC service with the given serviceName from
// the SUT.
func (t *SUTTargets) GRPC(ctx context.Context, serviceName string) (string, error) {
	if !t.sut.started {
		return "", ErrNotStarted
	}
	target, err := t.findTarget(ctx, &grpcInterface{
		serviceName: serviceName,
	})
	return string(target), err
}

// GRPC returns a connection to the gRPC service with the given serviceName from
// the SUT.
func (i *SUTInterfaces) GRPC(ctx context.Context, serviceName string) (*grpc.ClientConn, error) {
	if !i.sut.started {
		return nil, ErrNotStarted
	}

	i.mu.Lock()
	defer i.mu.Unlock()
	if conn, ok := i.grpcConnByServiceName[serviceName]; ok {
		return conn, nil
	}

	target, err := i.sut.Targets().GRPC(ctx, serviceName)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var conn *grpc.ClientConn
	retryPolicy := backoff.NewExponentialBackOff()
	retryPolicy.MaxElapsedTime = dialTimeout
	if err := backoff.Retry(func() error {
		// TODO(team): Change to secure gRPC servers.
		conn, err = grpc.DialContext(ctx, target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("%w: gRPC %s: %v", ErrDialGRPCService, serviceName, err)
		}
		return nil
	}, retryPolicy); err != nil {
		return nil, err
	}

	if err := backoff.Retry(func() error {
		err := verifyGRPCService(ctx, conn, serviceName)
		if err != nil {
			log.WarningContextf(ctx, "Failed to verify gRPC service: %v", err)
		}
		return err
	}, retryPolicy); err != nil {
		return nil, fmt.Errorf("%w: gRPC %s: %v", ErrVerifyGRPCService, serviceName, err)
	}

	i.grpcConnByServiceName[serviceName] = conn
	return conn, nil
}

func verifyGRPCService(ctx context.Context, conn *grpc.ClientConn, serviceName string) error {
	client, err := refgrpc.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return err
	}
	defer client.CloseSend()

	if err := client.Send(&refpb.ServerReflectionRequest{
		MessageRequest: &refpb.ServerReflectionRequest_ListServices{
			ListServices: serviceName,
		},
	}); err != nil {
		return err
	}

	resp, err := client.Recv()
	if err != nil {
		return err
	}
	var services []string
	switch t := resp.MessageResponse.(type) {
	case *refpb.ServerReflectionResponse_ListServicesResponse:
		for _, service := range t.ListServicesResponse.GetService() {
			services = append(services, service.GetName())
			if serviceName == service.GetName() {
				return nil
			}
		}
		return backoff.Permanent(fmt.Errorf("service %q not found: %v", serviceName, services))
	case *refpb.ServerReflectionResponse_ErrorResponse:
		return fmt.Errorf("error received from service: %v (%d)", t.ErrorResponse.GetErrorMessage(), t.ErrorResponse.GetErrorCode())
	default:
		return fmt.Errorf("unexpected response from service: %v", resp.MessageResponse)
	}
}

// GRPCSelector returns a function that matches a gRPC interface with the given
// service name.
func GRPCSelector(serviceName string) func(*monaxpb.Interface) bool {
	return func(intf *monaxpb.Interface) bool {
		return intf.GetGrpc().GetServiceName() == serviceName
	}
}
