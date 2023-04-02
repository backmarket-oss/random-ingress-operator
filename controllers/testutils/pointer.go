/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package testutils

// Copied straight from https://github.com/kubernetes/utils/blob/master/pointer/pointer.go
// To avoid pulling-in the unreleased k8s.io/utils module.

// BoolPtr returns a pointer to a bool
func BoolPtr(b bool) *bool {
	return &b
}
