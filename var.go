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
	"os"
	"os/user"
	"strings"
	"text/template"

	"golang.org/x/exp/maps"
)

const (
	monaxNameKey = "MONAX_NAME"
)

var (
	errSubstituteVars = errors.New("substitute vars")
)

func init() {
	setEnv()
}

func setEnv() {
	// If the user set a value, even if it's empty, use it as is.
	if _, ok := os.LookupEnv(monaxNameKey); ok {
		return
	}

	// Otherwise, try to get the current username, with "sut" as a fallback.
	monaxName := func() string {
		if u, err := user.Current(); err == nil { // if NO error
			return u.Username
		}
		if u := os.Getenv("USER"); u != "" {
			return u
		}
		return "sut"
	}()
	os.Setenv(monaxNameKey, monaxName)
}

// ProcessVars replaces templated strings in the local map with the appropriate
// values from the global or local map or the appropriate dependency target.
func ProcessVars(global map[string]string, local map[string]string, component *Component) (map[string]string, error) {
	filledLocals := make(map[string]string, len(local))
	for key, value := range local {
		substitution, err := substituteVars(value, global, local, component)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errSubstituteVars, err)
		}
		filledLocals[key] = substitution
	}
	return filledLocals, nil
}

func substituteVars(text string, global map[string]string, local map[string]string, component *Component) (string, error) {
	funcMap := map[string]any{
		"global": func(key string) (string, error) {
			if global == nil {
				return "", fmt.Errorf("cannot fill global var %s because global vars are nil", key)
			}
			value, ok := global[key]
			if !ok {
				return "", fmt.Errorf("unknown global var %q: %v", key, maps.Keys(global))
			}
			return value, nil
		},
		"local": func(key string) (string, error) {
			value, ok := local[key]
			if !ok {
				return "", fmt.Errorf("unknown local var %q: %v", key, maps.Keys(local))
			}
			return value, nil
		},
		"target": func(key string) (string, error) {
			value, err := component.RequiredTarget(key)
			return string(value), err
		},
	}
	t, err := template.New(text).Funcs(funcMap).Parse(text)
	if err != nil {
		return "", fmt.Errorf("invalid template in string %q: %w", text, err)
	}
	var b strings.Builder
	if err = t.Execute(&b, nil); err != nil {
		return "", fmt.Errorf("invalid template in string %q: %w", text, err)
	}
	return os.ExpandEnv(b.String()), nil
}
