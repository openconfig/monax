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

// Division is a simple gRPC division server.
package main

import (
	"fmt"
	"net"

	"golang.org/x/net/context"

	"flag"

	log "github.com/golang/glog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	divisiongrpc "github.com/openconfig/monax/example/math/division/api"
	divisionpb "github.com/openconfig/monax/example/math/division/api"
	subtractiongrpc "github.com/openconfig/monax/example/math/subtraction/api"
	subtractionpb "github.com/openconfig/monax/example/math/subtraction/api"
)

var (
	port              = flag.Int("port", 50054, "The server port")
	subtractionServer = flag.String("subtraction_server", "localhost:50052", "The address of the subtraction server")
	subtractionClient subtractiongrpc.SubtractionClient
)

type server struct {
	divisiongrpc.UnimplementedDivisionServer
}

func (s *server) Divide(ctx context.Context, in *divisionpb.DivideRequest) (*divisionpb.DivideResponse, error) {
	dividend := in.GetDividend()
	divisor := in.GetDivisor()
	log.InfoContextf(ctx, "Received request for quotient of %v and %v", dividend, divisor)

	if divisor == 0 {
		return nil, status.Error(codes.InvalidArgument, "undefined")
	}

	negative := false
	if dividend < 0 {
		negative = !negative
		dividend = -dividend
	}
	if divisor < 0 {
		negative = !negative
		divisor = -divisor
	}

	quotient := int64(0)
	remainder := dividend
	for ; remainder >= divisor; quotient++ {
		log.InfoContextf(ctx, "Sending request for difference of %v and %v", remainder, divisor)
		response, err := subtractionClient.Subtract(ctx, subtractionpb.SubtractRequest_builder{
			Minuend:    proto.Int64(remainder),
			Subtrahend: proto.Int64(divisor),
		}.Build())
		if err != nil {
			return nil, err
		}

		remainder = response.GetDifference()
		log.InfoContextf(ctx, "Received response with difference of %v", remainder)
	}

	if negative {
		quotient = -quotient
	}

	log.InfoContextf(ctx, "Sending response with quotient of %v", quotient)
	return divisionpb.DivideResponse_builder{
		Quotient:  proto.Int64(quotient),
		Remainder: proto.Int64(remainder),
	}.Build(), nil
}

func main() {
	flag.Parse()

	a := fmt.Sprintf(":%d", *port)
	l, err := net.Listen("tcp", a)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	conn, err := grpc.Dial(*subtractionServer, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to the subtraction server: %v", err)
	}
	defer conn.Close()
	subtractionClient = subtractiongrpc.NewSubtractionClient(conn)

	s := grpc.NewServer()
	divisiongrpc.RegisterDivisionServer(s, &server{})
	reflection.Register(s)

	log.Infof("Server listening at %v", l.Addr())
	if err := s.Serve(l); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
