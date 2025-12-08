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

	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var (
	gatewayv1ServiceGroup gatewayv1.Group = ""
	gatewayv1ServiceKind  gatewayv1.Kind  = "Service"
)

// VectorAssignmentReconciler reconciles VectorAssignment resources where manifest type is either 'cloud.konfidence.flux.kustomize' or 'cloud.konfidence.flux.helm'
type VectorAssignmentReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=vectorassignments,verbs=get;list;watch
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=vectorassignments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *VectorAssignmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	manifestTypeFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *landscapev1alpha1.VectorAssignment:
			return obj.Spec.Manifest.Type == "cloud.konfidence.flux.kustomize" || obj.Spec.Manifest.Type == "cloud.konfidence.flux.helm"
		case *gatewayv1.HTTPRoute, *corev1.Service:
			return true
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&landscapev1alpha1.VectorAssignment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(manifestTypeFilter).
		Owns(&corev1.Service{}, builder.MatchEveryOwner).
		Owns(&gatewayv1.HTTPRoute{}).
		Named("k8s_vectorassignment").
		Complete(r)
}

func (r *VectorAssignmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("start reconciling vector assignment")

	assignment := &landscapev1alpha1.VectorAssignment{}
	if err := r.Get(ctx, req.NamespacedName, assignment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get vector assignment object: %w", err)
	}

	originalAssignment := assignment.DeepCopy()
	patch := client.MergeFrom(originalAssignment)

	artifactDeployment := &landscapev1alpha1.ArtifactDeployment{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: assignment.Namespace,
		Name:      assignment.Spec.ArtifactDeploymentRef.Name,
	}, artifactDeployment)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could not get referenced artifact deployment: %w", err)
	}

	for _, deploymentResult := range artifactDeployment.Status.DeploymentResults {
		if deploymentResult.Type != "http-k8s-service" {
			continue
		}
		log.Info(fmt.Sprintf("reconciling artifact deployment result %q", deploymentResult.Name))

		route := ServiceRouteResult{
			Name: deploymentResult.Name,
		}
		err := json.Unmarshal(deploymentResult.Spec.Raw, &route.Service)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("could not unmarshal deploymentresult spec for %q: %w", deploymentResult.Name, err)
		}

		svc, err := r.ensureAppNameService(ctx, assignment, route)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("could not ensure app name service exists for %q: %w", deploymentResult.Name, err)
		}

		err = r.ensureHTTPRoute(ctx, assignment, svc, route)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("could not ensure httproute exists for %q: %w", deploymentResult.Name, err)
		}
	}

	if !reflect.DeepEqual(assignment.Status, originalAssignment.Status) {
		if err := r.Client.Status().Patch(ctx, assignment, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to patch vector assignment status: %w", err)
		}
	}

	log.Info("finish reconciling vector assignment")
	return ctrl.Result{}, nil
}

func (r *VectorAssignmentReconciler) ensureAppNameService(ctx context.Context, assignment *landscapev1alpha1.VectorAssignment, route ServiceRouteResult) (*corev1.Service, error) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      route.Name,
			Namespace: route.Service.Namespace,
		},
	}

	mutateFn := func() error {
		svc.Spec = corev1.ServiceSpec{
			Ports: route.Service.ServicePorts,
		}

		err := controllerutil.SetOwnerReference(assignment, svc, r.Scheme())
		if err != nil {
			return fmt.Errorf("failed to set owner reference on appName service: %w", err)
		}

		return nil
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, mutateFn)
	if err != nil {
		return nil, fmt.Errorf("failed to reconcile appName service: %w", err)
	}

	logf.FromContext(ctx).Info(fmt.Sprintf("appName service %q reconciled successfully", route.Name))

	return svc, nil
}

func (r *VectorAssignmentReconciler) ensureHTTPRoute(ctx context.Context, assignment *landscapev1alpha1.VectorAssignment, svc *corev1.Service, route ServiceRouteResult) error {
	httpRoute := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%s", route.Name, assignment.Spec.VectorDeploymentRef.Name, assignment.Spec.ArtifactDeploymentRef.Name),
			Namespace: route.Service.Namespace,
		},
	}

	mutateFn := func() error {
		httpRoute.Spec = gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Group:     &gatewayv1ServiceGroup,
						Kind:      &gatewayv1ServiceKind,
						Namespace: toNamespace(svc.Namespace),
						Name:      gatewayv1.ObjectName(svc.Name),
					},
				},
			},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Headers: []gatewayv1.HTTPHeaderMatch{
								{
									Name:  "X-Vector-ID",
									Value: assignment.Spec.VectorDeploymentRef.Name,
								},
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Group:     &gatewayv1ServiceGroup,
									Kind:      &gatewayv1ServiceKind,
									Namespace: toNamespace(route.Service.Namespace),
									Name:      gatewayv1.ObjectName(route.Service.K8sName),
									Port:      toPort(route.Service.ServicePorts[0].Port), // TODO (karsten / 2025-12-04): support multiple ports? this needs one HTTPRoute per port then
								},
							},
						},
					},
				},
			},
		}

		return controllerutil.SetControllerReference(assignment, httpRoute, r.Scheme())
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, httpRoute, mutateFn)
	if err != nil {
		return fmt.Errorf("failed to reconcile HTTPRoute: %w", err)
	}

	logf.FromContext(ctx).Info(fmt.Sprintf("HTTPRoute %q reconciled successfully", httpRoute.Name))

	r.mapStatusConditions(assignment, httpRoute)

	return nil
}

func (r *VectorAssignmentReconciler) mapStatusConditions(
	assignment *landscapev1alpha1.VectorAssignment,
	route *gatewayv1.HTTPRoute,
) {
	// The created HTTPRoute always has exactly one parent ref, which is the Service created for the application. We
	// consider the assignment to be ready when the HTTPRoute is accepted and all references are resolved.
	if len(route.Status.Parents) == 1 &&
		meta.IsStatusConditionTrue(route.Status.Parents[0].Conditions, string(gatewayv1.RouteConditionAccepted)) &&
		meta.IsStatusConditionTrue(route.Status.Parents[0].Conditions, string(gatewayv1.RouteConditionResolvedRefs)) {
		meta.SetStatusCondition(&assignment.Status.Conditions, metav1.Condition{
			Type:    landscapev1alpha1.VectorAssignedCondition,
			Status:  metav1.ConditionTrue,
			Reason:  "AssignmentReady",
			Message: "HTTPRoute has been accepted and all references resolved",
		})
	} else {
		meta.SetStatusCondition(&assignment.Status.Conditions, metav1.Condition{
			Type:    landscapev1alpha1.VectorAssignedCondition,
			Status:  metav1.ConditionFalse,
			Reason:  "AssignmentNotReady",
			Message: "HTTPRoute is either not accepted or has unresolved references",
		})
	}
}

func toNamespace(ns string) *gatewayv1.Namespace {
	gatewayNamespace := gatewayv1.Namespace(ns)
	return &gatewayNamespace
}

func toPort(p int32) *gatewayv1.PortNumber {
	return &p
}

type ServiceRouteResult struct {
	Name    string
	Service DeploymentResultServiceSpec
}
