/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkingv1alpha1 "github.com/BackMarket-oss/random-ingress-operator/api/v1alpha1"
	mock_client "github.com/BackMarket-oss/random-ingress-operator/controllers/mocks"
	"github.com/BackMarket-oss/random-ingress-operator/controllers/testutils"
)

const testMaxLifetime = 2 * time.Minute
const testGracePeriod = 10 * time.Second

func TestMain(m *testing.M) {
	err := networkingv1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatal("Failed to add custom resource to schema: ", err)
	}
	os.Exit(m.Run())
}

func TestRandomIngressReconciler_InvalidSpec(t *testing.T) {
	testCases := []struct {
		name            string
		input           networkingv1alpha1.IngressTemplateSpec
		expectedMessage string
	}{
		{
			name: "all hosts invalid",
			input: networkingv1alpha1.IngressTemplateSpec{
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{Host: "norandom"},
						{Host: "random|"},
						{Host: "random |"},
						{Host: "|random"},
						{Host: "| random"},
						{Host: "random"},
						{Host: ""},
					},
				},
			},
			expectedMessage: `[spec.ingressTemplate.spec.rules[0].host: Invalid value: "norandom": missing |RANDOM| placeholder, ` +
				`spec.ingressTemplate.spec.rules[1].host: Invalid value: "random|": missing |RANDOM| placeholder, ` +
				`spec.ingressTemplate.spec.rules[2].host: Invalid value: "random |": missing |RANDOM| placeholder, ` +
				`spec.ingressTemplate.spec.rules[3].host: Invalid value: "|random": missing |RANDOM| placeholder, ` +
				`spec.ingressTemplate.spec.rules[4].host: Invalid value: "| random": missing |RANDOM| placeholder, ` +
				`spec.ingressTemplate.spec.rules[5].host: Invalid value: "random": missing |RANDOM| placeholder, ` +
				`spec.ingressTemplate.spec.rules[6].host: Invalid value: "": missing |RANDOM| placeholder]`,
		},
		{
			name: "some hosts valid",
			input: networkingv1alpha1.IngressTemplateSpec{
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{Host: "|RANDOM|.example.com"},
						{Host: "norandom"},
					},
				},
			},
			expectedMessage: `spec.ingressTemplate.spec.rules[1].host: Invalid value: "norandom": missing |RANDOM| placeholder`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			randomIngress := networkingv1alpha1.RandomIngress{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testIngress",
					Namespace: "default",
				},
				Spec: networkingv1alpha1.RandomIngressSpec{
					IngressTemplate: tc.input,
				},
			}

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			testClient, statusClient := newClientMock(ctrl)
			expectGetRandomIngress(testClient, &randomIngress, nil)
			expectListIngresses(testClient, "default", "testIngress", []*networkingv1.Ingress{}, nil)
			_, actualStatus := expectUpdateStatus(statusClient, nil)

			clock := testutils.FakeClock{
				FixedNow: time.Date(2021, time.September, 06, 17, 12, 0, 0, time.UTC),
			}

			testUUIDSource := testutils.NewFakeUUIDSource(t, []types.UID{})

			reconciler := RandomIngressReconciler{
				Client:                  testClient,
				Scheme:                  scheme.Scheme,
				Clock:                   clock,
				UUIDSource:              testUUIDSource,
				IngressMaxLifetime:      testMaxLifetime,
				IngressHandoverDuration: testGracePeriod,
			}

			res, err := reconciler.Reconcile(context.Background(), newReq("default", "testIngress"))

			// Validation errors get reported through Status, they're not a failure
			// of the reconciler.
			assert.Nil(t, err)

			// Validation errors don't lead to periodic retry,
			// the spec needs fixing.
			assert.Zero(t, res)

			expectedStatus := &networkingv1alpha1.RandomIngressStatus{
				Conditions: []networkingv1alpha1.RandomIngressCondition{
					{
						Type:               networkingv1alpha1.RandomIngressValid,
						Status:             corev1.ConditionFalse,
						Reason:             specInvalidReason,
						LastHeartbeatTime:  metav1.Time{Time: clock.FixedNow},
						LastTransitionTime: metav1.Time{Time: clock.FixedNow},
						Message:            tc.expectedMessage,
					},
				},
			}

			assertStatusEquivalent(t, expectedStatus, actualStatus)
		})
	}
}

func TestRandomIngressReconciler_NoExistingIngress(t *testing.T) {
	randomIngress := testutils.ValidRandomIng.DeepCopy()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clock := testutils.FakeClock{
		FixedNow: time.Date(2021, time.September, 06, 17, 12, 0, 0, time.UTC),
	}

	returnedUUIDs := []types.UID{
		types.UID("6900d1a3-798c-4d9a-9a2f-737c72046efa"),
	}

	testUUIDSource := testutils.NewFakeUUIDSource(t, returnedUUIDs)

	nextRenewalTime := clock.FixedNow.Add(testMaxLifetime)

	expectedStatus := testutils.NewValidRandomIngStatus(clock.FixedNow, nextRenewalTime)

	testClient, statusClient := newClientMock(ctrl)
	updateStatusCall, actualStatus := expectUpdateStatus(statusClient, nil)
	createIngressCall, actualIngress := expectCreateIngress(testClient, nil)

	gomock.InOrder(
		expectGetRandomIngress(testClient, randomIngress, nil),
		expectListIngresses(testClient, "default", "randomIngress", []*networkingv1.Ingress{}, nil),
		createIngressCall,
		updateStatusCall,
	)

	reconciler := RandomIngressReconciler{
		Client:                  testClient,
		Scheme:                  scheme.Scheme,
		Clock:                   clock,
		UUIDSource:              testUUIDSource,
		IngressMaxLifetime:      testMaxLifetime,
		IngressHandoverDuration: testGracePeriod,
	}

	res, err := reconciler.Reconcile(context.Background(), newReq("default", "randomIngress"))
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, testMaxLifetime-testGracePeriod, res.RequeueAfter)

	assertStatusEquivalent(t, expectedStatus, actualStatus)
	assert.Len(t, testUUIDSource.Items, 0)

	assertIngressMatchesTemplate(t, randomIngress, actualIngress, returnedUUIDs[0])
}

func TestRandomIngressReconciler_AlreadyLiveIngress(t *testing.T) {
	randomIngress := testutils.ValidRandomIng.DeepCopy()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clock := testutils.FakeClock{
		FixedNow: time.Date(2021, time.September, 06, 17, 12, 0, 0, time.UTC),
	}

	testUUIDSource := testutils.NewFakeUUIDSource(t, []types.UID{})

	testClient, statusClient := newClientMock(ctrl)
	updateStatusCall, actualStatus := expectUpdateStatus(statusClient, nil)

	// Existing ingress is at the half of its lifetime: neither expired nor in handover period.
	halfMaxLifetime := testMaxLifetime / 2
	existingCreationTimestamp := clock.FixedNow.Add(-halfMaxLifetime)

	// In this case, we should requeue when we reach the grace period of the existing ingress, to create a new one.
	expectedRequeueAfter := testMaxLifetime - halfMaxLifetime - testGracePeriod
	expectedNextRenewalTime := existingCreationTimestamp.Add(testMaxLifetime)

	expectedStatus := testutils.NewValidRandomIngStatus(clock.FixedNow, expectedNextRenewalTime)

	existingIngress := testutils.ValidIngress.DeepCopy()
	existingIngress.CreationTimestamp = metav1.NewTime(existingCreationTimestamp)

	gomock.InOrder(
		expectGetRandomIngress(testClient, randomIngress, nil),
		expectListIngresses(testClient, "default", "randomIngress", []*networkingv1.Ingress{existingIngress}, nil),
		updateStatusCall,
	)

	reconciler := RandomIngressReconciler{
		Client:                  testClient,
		Scheme:                  scheme.Scheme,
		Clock:                   clock,
		UUIDSource:              testUUIDSource,
		IngressMaxLifetime:      testMaxLifetime,
		IngressHandoverDuration: testGracePeriod,
	}

	res, err := reconciler.Reconcile(context.Background(), newReq("default", "randomIngress"))
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, expectedRequeueAfter, res.RequeueAfter)

	assertStatusEquivalent(t, expectedStatus, actualStatus)
}

func TestRandomIngressReconciler_SpecChanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clock := testutils.FakeClock{
		FixedNow: time.Date(2021, time.September, 06, 17, 12, 0, 0, time.UTC),
	}

	testUUIDSource := testutils.NewFakeUUIDSource(t, []types.UID{
		"0d46c579-055a-42dd-b3c9-5a2418eeb44c",
	})

	testClient, statusClient := newClientMock(ctrl)
	updateStatusCall, actualStatus := expectUpdateStatus(statusClient, nil)

	// Existing ingress is at the half of its lifetime: neither expired nor in handover period.
	halfMaxLifetime := testMaxLifetime / 2
	existingCreationTimestamp := clock.FixedNow.Add(-halfMaxLifetime)

	// In this case, we should requeue when we reach the grace period of the new ingress, to create a new one.
	expectedRequeueAfter := testMaxLifetime - testGracePeriod
	expectedNextRenewalTime := clock.FixedNow.Add(testMaxLifetime)

	expectedStatus := testutils.NewValidRandomIngStatus(clock.FixedNow, expectedNextRenewalTime)

	randomIngress := testutils.ValidRandomIng.DeepCopy()
	newSpecRule := networkingv1.IngressRule{
		Host: "www.dev.|RANDOM|.example.com",
	}
	randomIngress.Spec.IngressTemplate.Spec.Rules = append(randomIngress.Spec.IngressTemplate.Spec.Rules, newSpecRule)

	existingIngressWrongSpec := testutils.ValidIngress.DeepCopy()
	existingIngressWrongSpec.CreationTimestamp = metav1.NewTime(existingCreationTimestamp)

	existingIngressWrongName := testutils.ValidIngress.DeepCopy()
	existingIngressWrongName.Name = "missing-a-dash"

	existingIngresses := []*networkingv1.Ingress{
		existingIngressWrongSpec,
		existingIngressWrongName,
	}

	createIngressCall, actualIngress := expectCreateIngress(testClient, nil)

	gomock.InOrder(
		expectGetRandomIngress(testClient, randomIngress, nil),
		expectListIngresses(testClient, "default", "randomIngress", existingIngresses, nil),
		expectDeleteIngress(testClient, existingIngressWrongSpec, nil),
		expectDeleteIngress(testClient, existingIngressWrongName, nil),
		createIngressCall,
		updateStatusCall,
	)

	reconciler := RandomIngressReconciler{
		Client:                  testClient,
		Scheme:                  scheme.Scheme,
		Clock:                   clock,
		UUIDSource:              testUUIDSource,
		IngressMaxLifetime:      testMaxLifetime,
		IngressHandoverDuration: testGracePeriod,
	}

	res, err := reconciler.Reconcile(context.Background(), newReq("default", "randomIngress"))
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, expectedRequeueAfter, res.RequeueAfter)
	assert.NotNil(t, actualIngress)

	assertStatusEquivalent(t, expectedStatus, actualStatus)
}

func TestRandomIngressReconciler_ExpiredIngress(t *testing.T) {
	randomIngress := testutils.ValidRandomIng.DeepCopy()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	clock := testutils.FakeClock{
		FixedNow: time.Date(2021, time.September, 06, 17, 12, 0, 0, time.UTC),
	}

	expectedIngressUID := types.UID("669f0808-4f8a-4aa5-a231-c7f7d313bfbe")
	testUUIDSource := testutils.NewFakeUUIDSource(t, []types.UID{expectedIngressUID})

	// Expired by 1 second
	existingCreationTimestamp := clock.FixedNow.Add(-testMaxLifetime).Add(-time.Second)

	// In this case, the old Ingress will be deleted immediately and won't matter in the requeue calculation.
	// We need to requeue at the handover period of the created Ingress.
	expectedNextRenewalTime := clock.FixedNow.Add(testMaxLifetime)
	expectedRequeueAfter := testMaxLifetime - testGracePeriod

	expectedStatus := testutils.NewValidRandomIngStatus(clock.FixedNow, expectedNextRenewalTime)

	existingIngress := testutils.ValidIngress.DeepCopy()
	existingIngress.CreationTimestamp = metav1.NewTime(existingCreationTimestamp)

	testClient, statusClient := newClientMock(ctrl)
	updateStatusCall, actualStatus := expectUpdateStatus(statusClient, nil)
	createIngressCall, actualIngress := expectCreateIngress(testClient, nil)

	expectedNewIngress := testutils.ValidIngress.DeepCopy()
	expectedNewIngress.Spec.Rules[0].Host = "669f0808-4f8a-4aa5-a231-c7f7d313bfbe.example.com"
	expectedNewIngress.Spec.Rules[1].Host = "www.669f0808-4f8a-4aa5-a231-c7f7d313bfbe.example.com"

	gomock.InOrder(
		expectGetRandomIngress(testClient, randomIngress, nil),
		expectListIngresses(testClient, "default", "randomIngress", []*networkingv1.Ingress{existingIngress}, nil),
		expectDeleteIngress(testClient, existingIngress, nil),
		createIngressCall,
		updateStatusCall,
	)

	reconciler := RandomIngressReconciler{
		Client:                  testClient,
		Scheme:                  scheme.Scheme,
		Clock:                   clock,
		UUIDSource:              testUUIDSource,
		IngressMaxLifetime:      testMaxLifetime,
		IngressHandoverDuration: testGracePeriod,
	}

	res, err := reconciler.Reconcile(context.Background(), newReq("default", "randomIngress"))
	assert.NoError(t, err)
	assert.NotNil(t, res)
	assert.Equal(t, expectedRequeueAfter, res.RequeueAfter)

	assertStatusEquivalent(t, expectedStatus, actualStatus)
	assertIngressMatchesTemplate(t, &testutils.ValidRandomIng, actualIngress, expectedIngressUID)
}

func newReq(namespace, name string) reconcile.Request {
	return reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		},
	}
}

// newClientMock creates a mock of Client with the Status sub-client also created, and returns both.
func newClientMock(ctrl *gomock.Controller) (*mock_client.MockClient, *mock_client.MockStatusWriter) {
	statusClient := mock_client.NewMockStatusWriter(ctrl)
	client := mock_client.NewMockClient(ctrl)
	client.EXPECT().Status().AnyTimes().Return(statusClient)

	return client, statusClient
}

func expectGetRandomIngress(mock *mock_client.MockClient, expectedOutput *networkingv1alpha1.RandomIngress, expectedErr error) *gomock.Call {
	key := client.ObjectKey{
		Namespace: expectedOutput.Namespace,
		Name:      expectedOutput.Name,
	}

	call := mock.EXPECT().Get(gomock.Not(gomock.Nil()), key, gomock.AssignableToTypeOf(expectedOutput)).
		DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...interface{}) error {
			if expectedOutput != nil {
				outObj := obj.(*networkingv1alpha1.RandomIngress)
				expectedOutput.DeepCopyInto(outObj)
			}
			return expectedErr
		})

	return call
}

func expectListIngresses(mock *mock_client.MockClient, namespace, ownerName string, expectedItems []*networkingv1.Ingress, expectedErr error) *gomock.Call {
	var list *networkingv1.IngressList
	call := mock.EXPECT().List(gomock.Not(gomock.Nil()), gomock.AssignableToTypeOf(list), client.InNamespace(namespace), client.MatchingFields{ingressOwnerKey: ownerName}).
		DoAndReturn(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
			outObj := list.(*networkingv1.IngressList)
			for _, item := range expectedItems {
				outObj.Items = append(outObj.Items, *item)
			}

			return expectedErr
		})

	return call
}

func expectUpdateStatus(mock *mock_client.MockStatusWriter, expectedErr error) (*gomock.Call, *networkingv1alpha1.RandomIngressStatus) {
	var r *networkingv1alpha1.RandomIngress

	result := networkingv1alpha1.RandomIngressStatus{}
	call := mock.EXPECT().Update(gomock.Not(gomock.Nil()), gomock.All(gomock.Not(gomock.Nil()), gomock.AssignableToTypeOf(r))).
		DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
			ingress := obj.(*networkingv1alpha1.RandomIngress)
			ingress.Status.DeepCopyInto(&result)

			return expectedErr
		})

	return call, &result
}

func expectCreateIngress(mock *mock_client.MockClient, expectedErr error) (*gomock.Call, *networkingv1.Ingress) {
	result := &networkingv1.Ingress{}

	call := mock.EXPECT().Create(gomock.Not(gomock.Nil()), gomock.All(gomock.Not(gomock.Nil()), gomock.AssignableToTypeOf(result))).
		DoAndReturn(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			ingress := obj.(*networkingv1.Ingress)
			ingress.DeepCopyInto(result)

			return expectedErr
		})

	return call, result
}

func expectDeleteIngress(mock *mock_client.MockClient, expectedIn *networkingv1.Ingress, expectedErr error) *gomock.Call {
	call := mock.EXPECT().Delete(gomock.Not(gomock.Nil()), gomock.Eq(expectedIn)).
		Return(expectedErr)

	return call
}

func assertStatusEquivalent(t *testing.T, expected, actual *networkingv1alpha1.RandomIngressStatus) {
	if expected == nil || actual == nil {
		if !assert.Equal(t, expected, actual) {
			return
		}
	}

	conditionsSorter := func(conds []networkingv1alpha1.RandomIngressCondition) func(i, j int) bool {
		return func(i, j int) bool {
			return conds[i].Type < conds[j].Type
		}
	}

	sort.SliceStable(expected.Conditions, conditionsSorter(expected.Conditions))
	sort.SliceStable(actual.Conditions, conditionsSorter(actual.Conditions))

	assert.Equal(t, expected, actual)
}

func assertIngressMatchesTemplate(t *testing.T, template *networkingv1alpha1.RandomIngress, actual *networkingv1.Ingress, randomId types.UID) {
	assert.NotNil(t, template)
	assert.NotNil(t, actual)

	assert.Equal(t, template.Namespace, actual.Namespace)
	assert.Equal(t, template.Spec.IngressTemplate.Metadata.Annotations, actual.Annotations)
	assert.Equal(t, template.Spec.IngressTemplate.Metadata.Labels, actual.Labels)

	// Expected format: randomingressname-123abcd-123abcd
	assert.Regexp(t, fmt.Sprintf(`^%s-[a-zA-Z0-9]+-[a-zA-Z0-9]+$`, template.Name), actual.Name)

	expectedRules := template.Spec.IngressTemplate.Spec.Rules
	if !assert.Equal(t, len(expectedRules), len(actual.Spec.Rules)) {
		return
	}

	for i := range expectedRules {
		expectedHost := strings.ReplaceAll(expectedRules[i].Host, randomPlaceholder, string(randomId))
		assert.Equal(t, expectedHost, actual.Spec.Rules[i].Host)

		assert.Equal(t, expectedRules[i].IngressRuleValue, actual.Spec.Rules[i].IngressRuleValue)
	}
}
