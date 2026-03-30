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
	monaxpb "github.com/openconfig/monax/proto"
)

// Runtime is an interface for runtime implementations.
type Runtime interface {
	Initialize(parameters *monaxpb.RuntimeParameters) error
	Handler(kind string) Handler
}

// NewRuntimeFn defines the function signature for creating new instances
// of a Runtime.  It's used to allow different runtime implementations to be
// registered and created dynamically.
type NewRuntimeFn func() (Runtime, error)
