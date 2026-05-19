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

package math_test

import (
	"testing"
	"time"

	"golang.org/x/net/context"

	"flag"

	log "github.com/golang/glog"
	"github.com/google/go-cmp/cmp"
	"github.com/openconfig/monax"
	"github.com/openconfig/monax/monaxtest"
	"github.com/openconfig/monax/runtime/kubernetesruntime"
	"google.golang.org/protobuf/proto"

	additiongrpc "github.com/openconfig/monax/example/math/addition/api"
	additionpb "github.com/openconfig/monax/example/math/addition/api"
	divisiongrpc "github.com/openconfig/monax/example/math/division/api"
	divisionpb "github.com/openconfig/monax/example/math/division/api"
	multiplicationgrpc "github.com/openconfig/monax/example/math/multiplication/api"
	multiplicationpb "github.com/openconfig/monax/example/math/multiplication/api"
	subtractiongrpc "github.com/openconfig/monax/example/math/subtraction/api"
	subtractionpb "github.com/openconfig/monax/example/math/subtraction/api"
)

var (
	config monax.Config
	sut    *monax.SUT
)

func init() {
	config.RegisterFlags(nil)
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	flag.Parse()
	defer log.Flush() // Ensures log files are written to.
	if testing.Short() {
		log.WarningContext(ctx, "Skipping test in short mode")
		return
	}

	newRuntimeFn := kubernetesruntime.New

	var err error
	sut, err = monaxtest.Start(ctx, &config, newRuntimeFn)
	if err != nil {
		log.ExitContextf(ctx, "Failed to start SUT: %v", err)
	}
	defer func() {
		if err := sut.Stop(ctx); err != nil {
			log.ErrorContextf(ctx, "Failed to stop SUT: %v", err)
		}
	}()

	m.Run()
}

func TestAddition(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if err := sut.Status(ctx); err != nil {
		t.Fatalf("SUT is unhealthy: %v", err)
	}

	conn, err := sut.Interfaces().GRPC(ctx, "monax.example.addition.Addition")
	if err != nil {
		t.Fatalf("Failed to get addition connection: %v", err)
	}
	additionClient := additiongrpc.NewAdditionClient(conn)

	tests := []struct {
		desc string
		a, b int64
		want int64
	}{
		{desc: "both positive", a: 1, b: 2, want: 3},
		{desc: "both negative", a: -1, b: -2, want: -3},
		{desc: "positive and zero", a: 1, b: 0, want: 1},
		{desc: "negative and zero", a: -1, b: 0, want: -1},
		{desc: "zero and positive", a: 0, b: 2, want: 2},
		{desc: "zero and negative", a: 0, b: -2, want: -2},
		{desc: "positive and negative", a: 1, b: -2, want: -1},
		{desc: "negative and positive", a: -1, b: 2, want: 1},
		{desc: "both zero", a: 0, b: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			response, err := additionClient.Add(ctx, additionpb.AddRequest_builder{
				Augend: proto.Int64(test.a),
				Addend: proto.Int64(test.b),
			}.Build())

			if err != nil {
				t.Fatalf("Add(%v, %v) failed: %v", test.a, test.b, err)
			}
			if diff := cmp.Diff(test.want, response.GetSum()); diff != "" {
				t.Errorf("Add(%v, %v) response diff (-want, +got):\n%v", test.a, test.b, diff)
			}
		})
	}
}

func TestDivision(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if err := sut.Status(ctx); err != nil {
		t.Fatalf("SUT is unhealthy: %v", err)
	}

	conn, err := sut.Interfaces().GRPC(ctx, "monax.example.division.Division")
	if err != nil {
		t.Fatalf("Failed to get division connection: %v", err)
	}
	divisionClient := divisiongrpc.NewDivisionClient(conn)

	tests := []struct {
		desc    string
		a, b    int64
		wantQ   int64
		wantR   int64
		wantErr bool
	}{
		{desc: "both positive", a: 1, b: 2, wantQ: 0, wantR: 1},
		{desc: "both negative", a: -1, b: -2, wantQ: 0, wantR: 1},
		{desc: "positive and zero", a: 1, b: 0, wantErr: true},
		{desc: "negative and zero", a: -1, b: 0, wantErr: true},
		{desc: "zero and positive", a: 0, b: 2, wantQ: 0, wantR: 0},
		{desc: "zero and negative", a: 0, b: -2, wantQ: 0, wantR: 0},
		{desc: "positive and negative", a: 1, b: -2, wantQ: 0, wantR: 1},
		{desc: "negative and positive", a: -1, b: 2, wantQ: 0, wantR: 1},
		{desc: "both zero", a: 0, b: 0, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			response, err := divisionClient.Divide(ctx, divisionpb.DivideRequest_builder{
				Dividend: proto.Int64(test.a),
				Divisor:  proto.Int64(test.b),
			}.Build())

			if (err == nil) == test.wantErr {
				t.Fatalf("Divide(%v, %v) got %v, wantErr %v", test.a, test.b, err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if diff := cmp.Diff(test.wantQ, response.GetQuotient()); diff != "" {
				t.Errorf("Divide(%v, %v) response quotient diff (-want, +got):\n%v", test.a, test.b, diff)
			}
			if diff := cmp.Diff(test.wantR, response.GetRemainder()); diff != "" {
				t.Errorf("Divide(%v, %v) response remainder diff (-want, +got):\n%v", test.a, test.b, diff)
			}
		})
	}
}

func TestMultiplication(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if err := sut.Status(ctx); err != nil {
		t.Fatalf("SUT is unhealthy: %v", err)
	}

	conn, err := sut.Interfaces().GRPC(ctx, "monax.example.multiplication.Multiplication")
	if err != nil {
		t.Fatalf("Failed to get multiplication connection: %v", err)
	}
	multiplicationClient := multiplicationgrpc.NewMultiplicationClient(conn)

	tests := []struct {
		desc string
		a, b int64
		want int64
	}{
		{desc: "both positive", a: 1, b: 2, want: 2},
		{desc: "both negative", a: -1, b: -2, want: 2},
		{desc: "positive and zero", a: 1, b: 0, want: 0},
		{desc: "negative and zero", a: -1, b: 0, want: 0},
		{desc: "zero and positive", a: 0, b: 2, want: 0},
		{desc: "zero and negative", a: 0, b: -2, want: 0},
		{desc: "positive and negative", a: 1, b: -2, want: -2},
		{desc: "negative and positive", a: -1, b: 2, want: -2},
		{desc: "both zero", a: 0, b: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			response, err := multiplicationClient.Multiply(ctx, multiplicationpb.MultiplyRequest_builder{
				Multiplier:   proto.Int64(test.a),
				Multiplicand: proto.Int64(test.b),
			}.Build())

			if err != nil {
				t.Fatalf("Multiply(%v, %v) failed: %v", test.a, test.b, err)
			}
			if diff := cmp.Diff(test.want, response.GetProduct()); diff != "" {
				t.Errorf("Multiply(%v, %v) response diff (-want, +got):\n%v", test.a, test.b, diff)
			}
		})
	}
}

func TestSubtraction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	if err := sut.Status(ctx); err != nil {
		t.Fatalf("SUT is unhealthy: %v", err)
	}

	conn, err := sut.Interfaces().GRPC(ctx, "monax.example.subtraction.Subtraction")
	if err != nil {
		t.Fatalf("Failed to get subtraction connection: %v", err)
	}
	subtractionClient := subtractiongrpc.NewSubtractionClient(conn)

	tests := []struct {
		desc string
		a, b int64
		want int64
	}{
		{desc: "both positive", a: 1, b: 2, want: -1},
		{desc: "both negative", a: -1, b: -2, want: 1},
		{desc: "positive and zero", a: 1, b: 0, want: 1},
		{desc: "negative and zero", a: -1, b: 0, want: -1},
		{desc: "zero and positive", a: 0, b: 2, want: -2},
		{desc: "zero and negative", a: 0, b: -2, want: 2},
		{desc: "positive and negative", a: 1, b: -2, want: 3},
		{desc: "negative and positive", a: -1, b: 2, want: -3},
		{desc: "both zero", a: 0, b: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()

			response, err := subtractionClient.Subtract(ctx, subtractionpb.SubtractRequest_builder{
				Minuend:    proto.Int64(test.a),
				Subtrahend: proto.Int64(test.b),
			}.Build())

			if err != nil {
				t.Fatalf("Subtract(%v, %v) failed: %v", test.a, test.b, err)
			}
			if diff := cmp.Diff(test.want, response.GetDifference()); diff != "" {
				t.Errorf("Subtract(%v, %v) response diff (-want, +got):\n%v", test.a, test.b, diff)
			}
		})
	}
}
