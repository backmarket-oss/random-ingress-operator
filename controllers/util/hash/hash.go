/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package hash

import (
	"encoding/hex"
	"hash"
	"hash/fnv"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/util/rand"

	networkingv1alpha1 "github.com/BackMarket-oss/random-ingress-operator/api/v1alpha1"
)

// Straightforward copy of
// https://github.com/kubernetes/kubernetes/blob/eb729620c522753bc7ae61fc2c7b7ea19d4aad2f/pkg/util/hash/hash.go#L28
// to avoid referencing k8s.io/kubernetes which is not expected to be pulled-in as dependency.
// (see https://github.com/kubernetes/kubernetes/issues/81878)

// DeepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func DeepHashObject(hasher hash.Hash, objectToWrite interface{}) {
	hasher.Reset()
	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}
	printer.Fprintf(hasher, "%#v", objectToWrite)
}

func RandomIngressSpec(spec *networkingv1alpha1.RandomIngressSpec) string {
	specHasher := fnv.New32a()
	DeepHashObject(specHasher, spec)
	return rand.SafeEncodeString(hex.EncodeToString(specHasher.Sum(nil)))
}
