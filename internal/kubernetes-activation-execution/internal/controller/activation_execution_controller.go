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

package controller

import (
	"context"
	"fmt"
	"reflect"

	landscape "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ActivationExecutionReconciler reconciles an ActivationExecution object
type ActivationExecutionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=activationexecutions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=activationexecutions/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ActivationExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile started...")

	// get activationExecution
	activationExecution := &landscape.ActivationExecution{}
	if err := r.Get(ctx, req.NamespacedName, activationExecution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalActivationExecution := activationExecution.DeepCopy()
	patch := client.MergeFrom(originalActivationExecution)
	err := r.reconcileActivationExecution(ctx, activationExecution)

	if !reflect.DeepEqual(activationExecution.Status, originalActivationExecution.Status) {
		if patchError := r.Client.Status().Patch(ctx, activationExecution, patch); patchError != nil {
			patchErrorMessage := "unable to update activationExecution status"

			if err != nil {
				reconcileError := fmt.Errorf("an error occurred while reconciling activationExecution: %w", err)
				return ctrl.Result{}, fmt.Errorf("%s: %w; %w", patchErrorMessage, patchError, reconcileError)
			}

			return ctrl.Result{}, fmt.Errorf("%s: %w", patchErrorMessage, patchError)
		}
	}

	return ctrl.Result{}, err
}

func (r *ActivationExecutionReconciler) reconcileActivationExecution(ctx context.Context, activationExecution *landscape.ActivationExecution) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling activationExecution")

	// TODO implement httpRoute, Gateway configuration based on activationExecution spec

	// mark activationExecution as successful
	meta.SetStatusCondition(&activationExecution.Status.Conditions, metav1.Condition{Type: landscape.ActivationExecutionSucceeded,
		Status: metav1.ConditionTrue, Reason: landscape.ActivationExecutionSucceeded,
		Message: fmt.Sprintf("Successfully reconciled ActivationExecution %s", activationExecution.Name)})

	log.Info("ActivationExecution reconciled")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActivationExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	activationExecutionFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch o := obj.(type) {
		// TODO specify type to listen for
		case *landscape.ActivationExecution:
			return o.Spec.Type == "k8s-job"
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&landscape.ActivationExecution{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(activationExecutionFilter).
		Named("activationExecution").
		Complete(r)
}
