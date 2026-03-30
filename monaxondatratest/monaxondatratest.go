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

// Package monaxondatratest provides a utility function to start and stop the
// SUT for Monax tests within the Ondatra test lifecycle.
package monaxondatratest

import (
	"fmt"

	"golang.org/x/net/context"

	log "github.com/golang/glog"
	"github.com/openconfig/monax"
	"github.com/openconfig/ondatra"
	"github.com/openconfig/ondatra/eventlis"
)

// Init creates the Monax SUT and schedules it to be started before the Ondatra
// tests run.
//
// If the SUT fails to create, an error will be returned.  Otherwise, the SUT is
// returned immediately, but will not be started and healthy until Ondatra
// begins running the test cases.  The SUT will be stopped automatically when
// the tests are complete.
//
// Example Usage:
//
//	var sut *monax.SUT
//
//	func TestMain(m *testing.M) {
//		ctx := context.Background()
//		var err error
//		sut, err = monaxondatratest.Init(ctx, &config, myRuntime.New)
//		if err != nil {
//			log.ExitContextf(ctx, "Failed to initialize SUT: %v", err)
//		}
//		ondatra.RunTests(m, mybindinit.Init)
//	}
//
//	func TestX(t *testing.T) {
//		ctx := t.Context()
//		if err := sut.Status(ctx); err != nil {
//				t.Fatalf("SUT is unhealthy: %v", err)
//		}
//		// use sut
//	}
func Init(ctx context.Context, config *monax.Config, newRuntimeFn monax.NewRuntimeFn) (*monax.SUT, error) {
	sut, err := monax.New(ctx, config, newRuntimeFn)
	if err != nil {
		return nil, fmt.Errorf("create SUT: %w", err)
	}

	ondatra.EventListener().AddBeforeTestsCallback(func(*eventlis.BeforeTestsEvent) error {
		if err := sut.Start(ctx); err != nil {
			log.ErrorContextf(ctx, "Failed to start SUT: %v", err)
			return fmt.Errorf("start SUT: %w", err)
		}

		if err := sut.Status(ctx); err != nil {
			log.ErrorContextf(ctx, "SUT is unhealthy: %v", err)
			return fmt.Errorf("unhealthy SUT: %w", err)
		}

		return nil
	})

	ondatra.EventListener().AddAfterTestsCallback(func(*eventlis.AfterTestsEvent) error {
		if err := sut.Stop(ctx); err != nil {
			log.ErrorContextf(ctx, "Failed to stop SUT: %v", err)
			return fmt.Errorf("stop SUT: %w", err)
		}

		return nil
	})

	return sut, nil
}
