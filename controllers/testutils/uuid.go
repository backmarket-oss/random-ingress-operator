/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package testutils

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

type FakeUUIDSource struct {
	t     *testing.T
	Items []types.UID
}

func (s *FakeUUIDSource) NewUUID() types.UID {
	require.Greater(s.t, len(s.Items), 0)

	res, tail := s.Items[0], s.Items[1:]
	s.Items = tail

	return res
}

func NewFakeUUIDSource(t *testing.T, items []types.UID) *FakeUUIDSource {
	return &FakeUUIDSource{
		t:     t,
		Items: items,
	}
}
