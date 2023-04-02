/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	networkingv1alpha1 "github.com/BackMarket-oss/random-ingress-operator/api/v1alpha1"
	//+kubebuilder:scaffold:imports
	"github.com/BackMarket-oss/random-ingress-operator/controllers/testutils"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var testClock testutils.FakeClock

const testIngressMaxLifetime = 10 * time.Minute

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)

	if !testing.Short() {
		RunSpecs(t,
			"Controller Suite")
	}
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = networkingv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	testClock = testutils.FakeClock{
		FixedNow: time.Date(2021, time.September, 06, 17, 12, 0, 0, time.UTC),
	}

	// Start our controller so it processes events on objects
	controller := &RandomIngressReconciler{
		Client:             k8sManager.GetClient(),
		Scheme:             k8sManager.GetScheme(),
		IngressMaxLifetime: testIngressMaxLifetime,
		Clock:              testClock,
	}

	err = controller.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = It("Should refuse to create Ingress when hosts are not random", func() {
	ctx := context.Background()
	randomIngress := &networkingv1alpha1.RandomIngress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "testrandomingress",
		},

		Spec: networkingv1alpha1.RandomIngressSpec{
			IngressTemplate: networkingv1alpha1.IngressTemplateSpec{
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: "fixed.example.com",
						},
						{
							Host: "www.|notreallyrandom|.example.com",
						},
						{
							Host: "www.| random|.example.com",
						},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, randomIngress)).Should(Succeed())

	var createdRandomIngress networkingv1alpha1.RandomIngress

	timeout := 1 * time.Second
	interval := 200 * time.Millisecond

	// Leave some time for the controller to get the creation event and update the status of the object.
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: randomIngress.Name, Namespace: randomIngress.Namespace}, &createdRandomIngress)
		if err != nil {
			return false
		}

		return len(createdRandomIngress.Status.Conditions) > 0
	}, timeout, interval).Should(BeTrue())

	Expect(createdRandomIngress.Status.NextRenewalTime.IsZero()).To(BeTrue())

	var actualValidCondition *networkingv1alpha1.RandomIngressCondition = nil
	for _, cond := range createdRandomIngress.Status.Conditions {
		if cond.Type == networkingv1alpha1.RandomIngressValid {
			actualValidCondition = &cond
			break
		}
	}

	Expect(actualValidCondition).NotTo(BeNil())
	Expect(actualValidCondition.Status).To(Equal(v1.ConditionFalse))
	Expect(actualValidCondition.Reason).To(Equal(specInvalidReason))
	Expect(actualValidCondition.Message).To(Equal(`[spec.ingressTemplate.spec.rules[0].host: Invalid value: "fixed.example.com": missing |RANDOM| placeholder, spec.ingressTemplate.spec.rules[1].host: Invalid value: "www.|notreallyrandom|.example.com": missing |RANDOM| placeholder, spec.ingressTemplate.spec.rules[2].host: Invalid value: "www.| random|.example.com": missing |RANDOM| placeholder]`))

	Consistently(func() bool {
		var ingresses networkingv1.IngressList
		err := k8sClient.List(ctx, &ingresses, client.InNamespace(randomIngress.Namespace))
		if err != nil {
			return false
		}

		for _, ingress := range ingresses.Items {
			for _, owner := range ingress.OwnerReferences {
				if owner.UID == createdRandomIngress.UID {
					return false
				}
			}
		}

		return true
	}, timeout, interval).Should(BeTrue())
})
