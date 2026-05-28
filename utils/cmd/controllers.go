/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package cmd provides shared startup wiring for operator binaries.
// It exposes FilterEnabledControllers for selecting which controllers
// a binary runs at startup, driven by the --controllers flag.
//
// The grammar for the --controllers flag is:
//
//	spec   = token ("," token)*
//	token  = ["!"] glob
//	glob   = a path.Match pattern, supporting "*", "?", and "[...]"
//
// Semantics are set-based and order-independent. Positive tokens contribute
// matching controllers to a union, negative tokens (prefixed with "!")
// contribute matching controllers to an exclusion set, and the result is
// (positives) minus (negatives). An empty spec or "*" enables all controllers.
package cmd // TODO: This is redundant to the code in kondifence/pkg/cmd, once the api is available we can reuse it

import (
	"fmt"
	"path"
	"strings"
)

// FilterEnabledControllers parses a comma-separated list of [!]<glob> tokens and returns the
// subset of registered names that should be enabled, as a membership set.
//
// Semantics are set-based and order-independent. Tokens partition into
// positives and negatives; the result is (union of positive matches) minus
// (union of negative matches). An empty spec or "*" yields all registered
// names. Glob matching uses path.Match, so "*", "?", and "[...]" are
// supported.
//
// Errors:
//   - a malformed glob (path.ErrBadPattern), and
//   - a literal token (no glob meta-characters) that matches zero registered
//     names — guards against typos and stale references to removed controllers.
func FilterEnabledControllers(spec string, controllerSetups map[string]func() error) (map[string]bool, error) {
	enabled := make(map[string]bool, len(controllerSetups))

	if spec == "" || spec == "*" {
		for name := range controllerSetups {
			enabled[name] = true
		}
		return enabled, nil
	}

	positive := map[string]bool{}
	negative := map[string]bool{}

	for _, raw := range strings.Split(spec, ",") {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}

		negate := false
		if strings.HasPrefix(token, "!") {
			negate = true
			token = strings.TrimSpace(token[1:])
			if token == "" {
				return nil, fmt.Errorf("invalid controller filter token: bare %q", "!")
			}
		}

		matched := false
		for name := range controllerSetups {
			ok, err := path.Match(token, name)
			if err != nil {
				return nil, fmt.Errorf("invalid controller filter glob %q: %w", token, err)
			}
			if !ok {
				continue
			}
			matched = true
			if negate {
				negative[name] = true
			} else {
				positive[name] = true
			}
		}

		if !matched && isLiteral(token) {
			return nil, fmt.Errorf("controller filter token %q matches no registered controller", token)
		}
	}

	for name := range positive {
		if !negative[name] {
			enabled[name] = true
		}
	}
	return enabled, nil
}

func isLiteral(token string) bool {
	return !strings.ContainsAny(token, "*?[")
}
