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

// Addition is a simple gRPC addition server.
package main

import (
	"fmt"
	"net"

	"golang.org/x/net/context"

	"flag"

	log "github.com/golang/glog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	additiongrpc "github.com/openconfig/monax/example/math/addition/api"
	additionpb "github.com/openconfig/monax/example/math/addition/api"
	"google.golang.org/protobuf/proto"
)

var (
	port = flag.Int("port", 50051, "The server port")
)

type server struct {
	additiongrpc.UnimplementedAdditionServer
}

func (s *server) Add(ctx context.Context, in *additionpb.AddRequest) (*additionpb.AddResponse, error) {
	augend := in.GetAugend()
	addend := in.GetAddend()
	log.InfoContextf(ctx, "Received request for sum of %v and %v", augend, addend)

	sum := augend + addend

	log.InfoContextf(ctx, "Sending response with sum of %v", sum)
	return additionpb.AddResponse_builder{
		Sum: proto.Int64(sum),
	}.Build(), nil
}

func main() {
	flag.Parse()

	a := fmt.Sprintf(":%d", *port)
	l, err := net.Listen("tcp", a)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	s := grpc.NewServer()
	additiongrpc.RegisterAdditionServer(s, &server{})
	reflection.Register(s)

	log.Infof("Server listening at %v", l.Addr())
	if err := s.Serve(l); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
