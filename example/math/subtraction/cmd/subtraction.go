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

// Subtraction is a simple gRPC subtraction server.
package main

import (
	"fmt"
	"net"

	"golang.org/x/net/context"

	"flag"

	log "github.com/golang/glog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"

	additiongrpc "github.com/openconfig/monax/example/math/addition/api"
	additionpb "github.com/openconfig/monax/example/math/addition/api"
	subtractiongrpc "github.com/openconfig/monax/example/math/subtraction/api"
	subtractionpb "github.com/openconfig/monax/example/math/subtraction/api"
	"google.golang.org/protobuf/proto"
)

var (
	port           = flag.Int("port", 50052, "The server port")
	additionServer = flag.String("addition_server", "localhost:50051", "The address of the addition server")
	additionClient additiongrpc.AdditionClient
)

type server struct {
	subtractiongrpc.UnimplementedSubtractionServer
}

func (s *server) Subtract(ctx context.Context, in *subtractionpb.SubtractRequest) (*subtractionpb.SubtractResponse, error) {
	minuend := in.GetMinuend()
	subtrahend := in.GetSubtrahend()
	log.InfoContextf(ctx, "Received request for difference between %v and %v", minuend, subtrahend)

	subtrahend = -subtrahend
	log.InfoContextf(ctx, "Sending request for sum of %v and %v", minuend, subtrahend)
	response, err := additionClient.Add(ctx, additionpb.AddRequest_builder{
		Augend: proto.Int64(minuend),
		Addend: proto.Int64(subtrahend),
	}.Build())
	if err != nil {
		return nil, err
	}

	difference := response.GetSum()
	log.InfoContextf(ctx, "Received response with sum of %v", difference)
	log.InfoContextf(ctx, "Sending response with difference of %v", difference)
	return subtractionpb.SubtractResponse_builder{
		Difference: proto.Int64(difference),
	}.Build(), nil
}

func main() {
	flag.Parse()

	a := fmt.Sprintf(":%d", *port)
	l, err := net.Listen("tcp", a)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	conn, err := grpc.Dial(*additionServer, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to connect to the addition server: %v", err)
	}
	defer conn.Close()
	additionClient = additiongrpc.NewAdditionClient(conn)

	s := grpc.NewServer()
	subtractiongrpc.RegisterSubtractionServer(s, &server{})
	reflection.Register(s)

	log.Infof("Server listening at %v", l.Addr())
	if err := s.Serve(l); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
