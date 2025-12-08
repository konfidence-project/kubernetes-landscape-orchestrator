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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// ActivationTaskExecutionReconciler reconciles an ActivationTaskExecution object
type ActivationTaskExecutionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	XVectorId                       = "x-vector-id"
	HttpActivationTaskExecutionType = "http-k8s-service"
)

// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=activationtaskexecutions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=activationtaskexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes/status,verbs=get;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *ActivationTaskExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconcile started...")

	// get activationTaskExecution
	activationTaskExecution := &landscape.ActivationTaskExecution{}
	if err := r.Get(ctx, req.NamespacedName, activationTaskExecution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// for now, we simply return an error if the execution does not contain any httpRouteConfigs
	if len(activationTaskExecution.Spec.HTTPRouteConfigs) == 0 {
		return ctrl.Result{}, fmt.Errorf("activationExecution %s does not contain any httpRoute configurations", activationTaskExecution.Name)
	}

	originalActivationTaskExecution := activationTaskExecution.DeepCopy()
	patch := client.MergeFrom(originalActivationTaskExecution)
	err := r.reconcileActivationTaskExecution(ctx, req, activationTaskExecution)

	if !reflect.DeepEqual(activationTaskExecution.Status, originalActivationTaskExecution.Status) {
		if patchError := r.Client.Status().Patch(ctx, activationTaskExecution, patch); patchError != nil {
			patchErrorMessage := "unable to update activationTaskExecution status"

			if err != nil {
				reconcileError := fmt.Errorf("an error occurred while reconciling activationTaskExecution: %w", err)
				return ctrl.Result{}, fmt.Errorf("%s: %w; %w", patchErrorMessage, patchError, reconcileError)
			}

			return ctrl.Result{}, fmt.Errorf("%s: %w", patchErrorMessage, patchError)
		}
	}

	return ctrl.Result{}, err
}

func (r *ActivationTaskExecutionReconciler) reconcileActivationTaskExecution(ctx context.Context, req ctrl.Request, activationTaskExecution *landscape.ActivationTaskExecution) error {
	log := logf.FromContext(ctx)
	log.Info("Reconciling activationTaskExecution")

	// get vectorActivation
	vectorActivation := &landscape.VectorActivation{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      activationTaskExecution.Spec.VectorActivation,
	}, vectorActivation)
	if err != nil {
		return fmt.Errorf("unable to fetch vectorActivation: %w", err)
	}

	// create httpRoutes based on execution config
	for _, httpRouteConfig := range activationTaskExecution.Spec.HTTPRouteConfigs {
		_, err := r.getOrCreateHttpRoute(ctx, req, httpRouteConfig, vectorActivation)
		if err != nil {
			return err
		}
	}

	// mark activationTaskExecution as successful
	meta.SetStatusCondition(&activationTaskExecution.Status.Conditions, metav1.Condition{Type: landscape.ActivationTaskExecutionSucceeded,
		Status: metav1.ConditionTrue, Reason: landscape.ActivationTaskExecutionSucceeded,
		Message: fmt.Sprintf("Successfully reconciled ActivationTaskExecution %s", activationTaskExecution.Name)})

	log.Info("ActivationTaskExecution reconciled")
	return nil
}

func (r *ActivationTaskExecutionReconciler) getOrCreateHttpRoute(ctx context.Context, req ctrl.Request, httpRouteConfig landscape.HTTPRouteConfig, vectorActivation *landscape.VectorActivation) (*gwapiv1.HTTPRoute, error) {
	log := logf.FromContext(ctx)

	httpRoute := &gwapiv1.HTTPRoute{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      httpRouteConfig.HTTPRouteName,
	}, httpRoute)

	if err != nil && !errors.IsNotFound(err) {
		return nil, fmt.Errorf("unable to fetch httpRoute: %w", err)
	}

	if err != nil && errors.IsNotFound(err) {
		log.Info("No matching httpRoute found. Creating a new one...")

		// create new httpRoute
		httpRoute, err := r.constructHttpRoute(req, httpRouteConfig, vectorActivation)
		if err != nil {
			return nil, fmt.Errorf("unable to construct httpRoute: %w", err)
		}

		if err := r.Create(ctx, httpRoute); err != nil {
			return nil, fmt.Errorf("unable to create httpRoute: %w", err)
		}
		log.Info("Created httpRoute", "httpRoute", httpRoute)
	}

	// TODO if the httpRoute already exists check that the spec is valid and matches the parameters and that the ownerref is set
	return httpRoute, nil
}

func (r *ActivationTaskExecutionReconciler) constructHttpRoute(req ctrl.Request, httpRouteConfig landscape.HTTPRouteConfig, vectorActivation *landscape.VectorActivation) (*gwapiv1.HTTPRoute, error) {
	headerMatchType := gwapiv1.HeaderMatchExact
	httpRoute := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      httpRouteConfig.HTTPRouteName,
			Namespace: req.Namespace,
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name: gwapiv1.ObjectName(httpRouteConfig.GatewayName),
					},
				},
			},
			Hostnames: []gwapiv1.Hostname{
				gwapiv1.Hostname(httpRouteConfig.HostName),
			},
			Rules: []gwapiv1.HTTPRouteRule{
				{
					Matches: []gwapiv1.HTTPRouteMatch{
						{
							Headers: []gwapiv1.HTTPHeaderMatch{
								{
									Type:  &headerMatchType,
									Name:  XVectorId,
									Value: httpRouteConfig.VectorID,
								},
							},
						},
					},
					BackendRefs: []gwapiv1.HTTPBackendRef{
						{
							BackendRef: gwapiv1.BackendRef{
								BackendObjectReference: gwapiv1.BackendObjectReference{
									Name: gwapiv1.ObjectName(httpRouteConfig.ServiceName),
									Port: &httpRouteConfig.Port,
								},
							},
						},
					},
				},
			},
		},
	}

	// set vectorActivation as owner of the httpRoute
	if err := controllerutil.SetOwnerReference(vectorActivation, httpRoute, r.Scheme); err != nil {
		return nil, fmt.Errorf("unable to set owner reference for httpRoute: %w", err)
	}

	return httpRoute, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActivationTaskExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	activationTaskExecutionFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch o := obj.(type) {
		case *landscape.ActivationTaskExecution:
			return o.Spec.Type == HttpActivationTaskExecutionType
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&landscape.ActivationTaskExecution{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(activationTaskExecutionFilter).
		Named("activationTaskExecution").
		Complete(r)
}
