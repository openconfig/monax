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

// Package monaxtest provides a utility function to start the SUT for Monax
// tests.
package monaxtest

import (
	"errors"
	"fmt"

	"golang.org/x/net/context"

	"github.com/openconfig/monax"
)

// Start creates, starts, and checks the health of the Monax SUT.
//
// If the SUT fails to start or is unhealthy after startup, it will be stopped
// and an error will be returned.  Otherwise, the SUT is returned.  The caller
// is responsible for stopping the SUT.
//
// Example Usage:
//
//	var sut *monax.SUT
//
//	func TestMain(m *testing.M) {
//		ctx := context.Background()
//		var err error
//		sut, err = monaxtest.Start(ctx, &config, myRuntime.New)
//		if err != nil {
//			log.ExitContextf(ctx, "Failed to start SUT: %v", err)
//		}
//		defer func() {
//			if err := sut.Stop(ctx); err != nil {
//				log.ErrorContextf(ctx, "Failed to stop SUT: %v", err)
//			}
//		}()
//		m.Run()
//	}
//
//	func TestX(t *testing.T) {
//		ctx := t.Context()
//		if err := sut.Status(ctx); err != nil {
//				t.Fatalf("SUT is unhealthy: %v", err)
//		}
//		// use sut
//	}
func Start(ctx context.Context, config *monax.Config, newRuntimeFn monax.NewRuntimeFn) (*monax.SUT, error) {
	sut, err := monax.New(ctx, config, newRuntimeFn)
	if err != nil {
		return nil, fmt.Errorf("create SUT: %w", err)
	}

	if err := sut.Start(ctx); err != nil {
		errs := fmt.Errorf("start SUT: %w", err)
		if err := sut.Stop(ctx); err != nil {
			errs = errors.Join(errs, fmt.Errorf("stop SUT: %w", err))
		}
		return nil, errs
	}

	if err := sut.Status(ctx); err != nil {
		errs := fmt.Errorf("unhealthy SUT: %w", err)
		if err := sut.Stop(ctx); err != nil {
			errs = errors.Join(errs, fmt.Errorf("stop SUT: %w", err))
		}
		return nil, errs
	}

	return sut, nil
}
