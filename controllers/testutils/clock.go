/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package testutils

import "time"

type FakeClock struct {
	FixedNow time.Time
}

func (c FakeClock) Now() time.Time { return c.FixedNow }
