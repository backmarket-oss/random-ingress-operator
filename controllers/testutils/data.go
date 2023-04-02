/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package testutils

import (
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	networkingv1alpha1 "github.com/BackMarket-oss/random-ingress-operator/api/v1alpha1"
	"github.com/BackMarket-oss/random-ingress-operator/controllers/util/hash"
)

// 	return gvk.Group + "/" + gvk.Version + ", Kind=" + gvk.Kind

var (
	TestNamespace = "default"

	ValidRandomIngName = "randomIngress"
	ValidRandomIngUID  = types.UID("879a95f7-8905-4412-8b95-0bbfd0c7351f")
	ValidRandomIng     = networkingv1alpha1.RandomIngress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ValidRandomIngName,
			Namespace: TestNamespace,
			UID:       ValidRandomIngUID,
		},
		Spec: networkingv1alpha1.RandomIngressSpec{
			IngressTemplate: networkingv1alpha1.IngressTemplateSpec{
				Metadata: networkingv1alpha1.IngressTemplateMetadata{
					Labels: map[string]string{
						"labelOne": "label1",
						"labelTwo": "label2",
					},

					Annotations: map[string]string{
						"annotationOne": "anno1",
						"annotationTwo": "anno2",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "|RANDOM|.example.com",
						},
						{
							Host: "www.|RANDOM|.example.com",
						},
					},
				},
			},
		},
	}

	ValidIngressName = fmt.Sprintf("%s-%s-123abc45", ValidRandomIngName, hash.RandomIngressSpec(&ValidRandomIng.Spec))
	ValidIngressUUID = "2bc112e6-c232-43ab-a658-2e29c21a6695"
	ValidIngress     = networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ValidIngressName,
			Namespace: TestNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "networking.backmarket.io/v1alpha1",
					Kind:               "RandomIngress",
					Name:               ValidRandomIngName,
					UID:                ValidRandomIngUID,
					Controller:         BoolPtr(true),
					BlockOwnerDeletion: BoolPtr(true),
				},
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: fmt.Sprintf("%s.example.com", ValidIngressUUID),
				},
				{
					Host: fmt.Sprintf("www.%s.example.com", ValidIngressUUID),
				},
			},
		},
	}
)

func NewValidRandomIngStatus(lastTransition, nextRenewal time.Time) *networkingv1alpha1.RandomIngressStatus {
	nextRenewalTime := metav1.NewTime(nextRenewal)

	s := &networkingv1alpha1.RandomIngressStatus{
		Conditions: []networkingv1alpha1.RandomIngressCondition{
			{
				Type:               networkingv1alpha1.RandomIngressValid,
				Status:             corev1.ConditionTrue,
				Reason:             "SpecValid",
				Message:            "spec is valid",
				LastHeartbeatTime:  metav1.NewTime(lastTransition),
				LastTransitionTime: metav1.NewTime(lastTransition),
			},
		},

		NextRenewalTime: &nextRenewalTime,
	}

	return s
}
