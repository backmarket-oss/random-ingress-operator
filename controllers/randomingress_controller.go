/*
Copyright 2022 the random-ingress-operator authors.
SPDX-License-Identifier: Apache-2.0
*/

package controllers

import (
	"context"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/BackMarket-oss/random-ingress-operator/api/v1alpha1"
	"github.com/BackMarket-oss/random-ingress-operator/controllers/util/hash"
)

const (
	randomPlaceholder             = "|RANDOM|"
	randomPlaceholderMissingError = "missing |RANDOM| placeholder"

	specValidReason   = "SpecValid"
	specInvalidReason = "SpecInvalid"
	specValidMessage  = "spec is valid"
)

// RandomIngressReconciler reconciles a RandomIngress object
type RandomIngressReconciler struct {
	Client                  client.Client
	Scheme                  *runtime.Scheme
	IngressMaxLifetime      time.Duration
	IngressHandoverDuration time.Duration
	Clock                   Clock
	UUIDSource              UUIDSource
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Clock represents a time source.
// It can be used to fake out timing for testing.
type Clock interface {
	Now() time.Time
}

// UUIDSource represents a source of random UUIDs.
// It can be used in tests to predetermine the returned UUIDs.
type UUIDSource interface {
	NewUUID() types.UID
}

type realUUIDSource struct{}

func (realUUIDSource) NewUUID() types.UID { return uuid.NewUUID() }

//+kubebuilder:rbac:groups=networking.backmarket.io,resources=randomingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.backmarket.io,resources=randomingresses/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=networking.backmarket.io,resources=randomingresses/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RandomIngress object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *RandomIngressReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("resource", req.NamespacedName)

	var randomIngress networkingv1alpha1.RandomIngress
	if err := r.Client.Get(ctx, req.NamespacedName, &randomIngress); err != nil {
		logger.Error(err, "failed to fetch RandomIngress")

		// No retry if the resource has been deleted.
		// Garbage collection based on OwnerReference will delete orphaned Ingress resources.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	randomIngress = *randomIngress.DeepCopy()
	logger.Info("Start processing")

	randomIngress.Status.NextRenewalTime = nil
	validationErrors := validateSpec(&randomIngress.Spec)
	if validationErrors != nil {
		message := validationErrors.ToAggregate().Error()

		logger.Info("Spec invalid", "validationErrors", message)

		invalidCondition := r.newCondition(networkingv1alpha1.RandomIngressValid, corev1.ConditionFalse, specInvalidReason, message)
		setCondition(&randomIngress.Status, invalidCondition)
	} else {
		validCondition := r.newCondition(networkingv1alpha1.RandomIngressValid, corev1.ConditionTrue, specValidReason, "spec is valid")
		setCondition(&randomIngress.Status, validCondition)
	}

	var ownedIngresses networkingv1.IngressList
	err := r.Client.List(ctx, &ownedIngresses, client.InNamespace(req.Namespace), client.MatchingFields{ingressOwnerKey: req.Name})
	if err != nil {
		logger.Error(err, "failed to list owned Ingresses")
		return ctrl.Result{}, err
	}

	specHash := hash.RandomIngressSpec(&randomIngress.Spec)

	var expiredIngresses []*networkingv1.Ingress
	var fullyAliveIngresses []*networkingv1.Ingress

	for i := range ownedIngresses.Items {
		switch {
		case !ingressMatchesSpec(&ownedIngresses.Items[i], specHash),
			r.ingressExpired(&ownedIngresses.Items[i]):
			expiredIngresses = append(expiredIngresses, &ownedIngresses.Items[i])
		case r.ingressExpiringSoon(&ownedIngresses.Items[i]):
			// We just need to exclude them from alive Ingresses so that they don't block
			// new Ingress creation.
		default:
			fullyAliveIngresses = append(fullyAliveIngresses, &ownedIngresses.Items[i])
		}
	}

	for _, ingress := range expiredIngresses {
		err := r.Client.Delete(ctx, ingress)
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "failed to delete expired Ingress", "ingressName", ingress.Name)
		} else {
			logger.Info("deleted expired Ingress", "ingressName", ingress.Name)
		}
	}

	var newIngress *networkingv1.Ingress = nil

	if len(fullyAliveIngresses) == 0 && len(validationErrors) == 0 {
		newIngress, err = r.createIngress(&randomIngress, specHash)
		if err != nil {
			logger.Error(err, "failed to create new Ingress")
			return ctrl.Result{}, err
		}
	}

	if newIngress != nil {
		err := r.Client.Create(ctx, newIngress)
		if err != nil {
			logger.Error(err, "failed to create Ingress for RandomIngress", "ingressName", newIngress.Name)
			return ctrl.Result{}, err
		}

		nextRenewalTime := metav1.NewTime(r.Clock.Now().Add(r.IngressMaxLifetime))
		randomIngress.Status.NextRenewalTime = &nextRenewalTime
	} else {
		if len(fullyAliveIngresses) > 0 {
			sort.Slice(fullyAliveIngresses, func(i, j int) bool {
				// Want the highest timestamp first
				return fullyAliveIngresses[i].CreationTimestamp.After(fullyAliveIngresses[j].CreationTimestamp.Time)
			})

			nextRenewalTime := metav1.NewTime(fullyAliveIngresses[0].CreationTimestamp.Time.Add(r.IngressMaxLifetime))
			randomIngress.Status.NextRenewalTime = &nextRenewalTime
		}
	}

	if err := r.Client.Status().Update(ctx, &randomIngress); err != nil {
		logger.Error(err, "failed to update Status")

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	result := ctrl.Result{}
	if randomIngress.Status.NextRenewalTime != nil {
		// TODO: we need to compute this from oldest Ingress creation time if there's one,
		// or we might not delete it in time.
		result.RequeueAfter = randomIngress.Status.NextRenewalTime.Time.Sub(r.Clock.Now()) - r.IngressHandoverDuration
	}

	logger.WithValues("requeueAfter", result.RequeueAfter).Info("Processed succesfully")
	return result, nil
}

func validateSpec(spec *networkingv1alpha1.RandomIngressSpec) (errs field.ErrorList) {
	rulesPath := field.NewPath("spec", "ingressTemplate", "spec", "rules")

	for i, rule := range spec.IngressTemplate.Spec.Rules {
		if !strings.Contains(rule.Host, randomPlaceholder) {
			hostPath := rulesPath.Index(i).Child("host")
			errs = append(errs, field.Invalid(hostPath, rule.Host, randomPlaceholderMissingError))
		}
	}

	return errs
}

func ingressMatchesSpec(ingress *networkingv1.Ingress, expectedSpecHash string) bool {
	nameParts := strings.Split(ingress.Name, "-")

	if len(nameParts) < 3 {
		return false
	}

	actualSpecHash := nameParts[len(nameParts)-2]

	return actualSpecHash == expectedSpecHash
}

func (r *RandomIngressReconciler) createIngress(randomIngress *networkingv1alpha1.RandomIngress, specHash string) (*networkingv1.Ingress, error) {
	randomHostpart := r.UUIDSource.NewUUID()

	// Needs to be part of the Ingress name to avoid collisions with previous
	// instances of the Ingress that have the same template.
	uuidHasher := fnv.New32a()
	uuidHasher.Write([]byte(randomHostpart))
	uuidHash := rand.SafeEncodeString(hex.EncodeToString(uuidHasher.Sum(nil)))
	ingressName := fmt.Sprintf("%s-%s-%s", randomIngress.Name, specHash, uuidHash)

	ingressSpec := randomIngress.Spec.IngressTemplate.Spec.DeepCopy()

	for i := range ingressSpec.Rules {
		rule := &ingressSpec.Rules[i]
		rule.Host = strings.ReplaceAll(rule.Host, randomPlaceholder, string(randomHostpart))
	}

	result := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ingressName,
			Namespace:   randomIngress.Namespace,
			Labels:      randomIngress.Spec.IngressTemplate.Metadata.Labels,
			Annotations: randomIngress.Spec.IngressTemplate.Metadata.Annotations,
		},
		Spec: *ingressSpec,
	}

	err := ctrl.SetControllerReference(randomIngress, result, r.Scheme)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// TODO: simplify this. See if we can get rid of pointer type.
func getCondition(status networkingv1alpha1.RandomIngressStatus, condType networkingv1alpha1.RandomIngressConditionType) *networkingv1alpha1.RandomIngressCondition {
	for _, cond := range status.Conditions {
		if cond.Type == condType {
			return &cond
		}
	}

	return nil
}

// TODO: simplify this. Code paths are hard to understand, "if currentCond != nil ..." especially.
func setCondition(status *networkingv1alpha1.RandomIngressStatus, condition networkingv1alpha1.RandomIngressCondition) {
	currentCond := getCondition(*status, condition.Type)
	if currentCond != nil && currentCond.Status == condition.Status && currentCond.Reason == condition.Reason && currentCond.Message == condition.Message {
		return
	}

	// Only update transition time if the status actually changed.
	if currentCond != nil && currentCond.Status == condition.Status {
		condition.LastTransitionTime = currentCond.LastTransitionTime
	}

	for i, cond := range status.Conditions {
		if cond.Type == condition.Type {
			status.Conditions[i] = condition
			return
		}
	}

	status.Conditions = append(status.Conditions, condition)
}

func (r *RandomIngressReconciler) newCondition(condType networkingv1alpha1.RandomIngressConditionType, status corev1.ConditionStatus, reason, message string) networkingv1alpha1.RandomIngressCondition {
	now := metav1.NewTime(r.Clock.Now())

	return networkingv1alpha1.RandomIngressCondition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastHeartbeatTime:  now,
		LastTransitionTime: now,
	}
}

// ingressExpired returns true if the input ingress has passed the maximum lifetime.
func (r *RandomIngressReconciler) ingressExpired(ingress *networkingv1.Ingress) bool {
	oldestAcceptableCreation := r.Clock.Now().Add(-r.IngressMaxLifetime)
	return ingress.CreationTimestamp.Time.Before(oldestAcceptableCreation)
}

// ingressExpiringSoon returns true if the input ingress is within IngressHandoverDuration of its expiration.
func (r *RandomIngressReconciler) ingressExpiringSoon(ingress *networkingv1.Ingress) bool {
	oldestAcceptableCreation := r.Clock.Now().Add(-r.IngressMaxLifetime).Add(r.IngressHandoverDuration)
	return ingress.CreationTimestamp.Time.Before(oldestAcceptableCreation)
}

const ingressOwnerKey = ".metadata.controller"

// SetupWithManager sets up the controller with the Manager.
func (r *RandomIngressReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Clock == nil {
		r.Clock = realClock{}
	}

	if r.UUIDSource == nil {
		r.UUIDSource = realUUIDSource{}
	}

	var ourAPIVersion = networkingv1alpha1.GroupVersion.String()

	// Setup a memory index on Ingress objects, keyed by the owning RandomIngress, so we can easily query them when reconciling.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &networkingv1.Ingress{}, ingressOwnerKey, func(obj client.Object) []string {
		owner := metav1.GetControllerOf(obj)
		if owner == nil {
			return nil
		}

		// Make sure it's one of ours
		if owner.APIVersion != ourAPIVersion || owner.Kind != "RandomIngress" {
			return nil
		}

		// And if so, return its name so the Ingress is indexed by its controlling RandomIngress
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.RandomIngress{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
