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
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	monaxpb "github.com/openconfig/monax/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"
)

func TestSetRuntimeParameters(t *testing.T) {
	t.Parallel()

	setRuntimeParametersErr := errors.New("error")

	tests := map[string]struct {
		runtimeParameters       *monaxpb.RuntimeParameters
		setRuntimeParametersErr error
		wantErr                 error
	}{
		"failed set runtime parameters": {
			runtimeParameters:       new(monaxpb.RuntimeParameters),
			setRuntimeParametersErr: setRuntimeParametersErr,
			wantErr:                 setRuntimeParametersErr,
		},
		"pass with runtime parameters": {
			runtimeParameters: monaxpb.RuntimeParameters_builder{
				// The anypb does not use Opaque API like editions 2024.
				Parameters: &anypb.Any{TypeUrl: "type_url", Value: []byte("value")},
			}.Build(),
		},
		"pass without runtime parameters": {
			runtimeParameters: nil,
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			runtime := &fakeRuntime{
				SetParametersErr: test.setRuntimeParametersErr,
			}

			err := runtime.Initialize(test.runtimeParameters)

			if !errors.Is(err, test.wantErr) {
				t.Errorf("setRuntimeParameters: error diff: want %v, got %v", test.wantErr, err)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(test.runtimeParameters, runtime.Parameters, protocmp.Transform()); diff != "" {
				t.Errorf("setRuntimeParameters: components diff (-want +got):\n%s", diff)
			}
		})
	}
}
