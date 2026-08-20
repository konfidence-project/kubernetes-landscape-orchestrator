package controller

import (
	"context"
	"fmt"
	"reflect"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const VectorAssignmentControllerName = "flux-vector-assignment-controller"

// VectorAssignmentReconciler reconciles VectorAssignment resources where manifest type is either
// 'cloud.konfidence.flux.kustomize' or 'cloud.konfidence.flux.helm'. East-west routing is carried by deployment
// results in VectorData, so the deployer no longer creates routing objects; it only reflects whether the referenced
// ArtifactDeployment has published its deployment results.
type VectorAssignmentReconciler struct {
	client.Client
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectorassignments,verbs=get;list;watch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectorassignments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch

// SetupWithManager sets up the controller with the Manager.
func (r *VectorAssignmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	manifestTypeFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		assignment, ok := obj.(*konfidencev1alpha1.VectorAssignment)
		if !ok {
			return true
		}
		return assignment.Spec.Manifest.Type == ManifestTypeKustomize || assignment.Spec.Manifest.Type == ManifestTypeHelm
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.VectorAssignment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&konfidencev1alpha1.ArtifactDeployment{}, handler.EnqueueRequestsFromMapFunc(r.assignmentsForArtifactDeployment)).
		WithEventFilter(manifestTypeFilter).
		Named("k8s_vectorassignment").
		Complete(r)
}

func (r *VectorAssignmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	assignment := &konfidencev1alpha1.VectorAssignment{}
	if err := r.Get(ctx, req.NamespacedName, assignment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get vector assignment object: %w", err)
	}

	original := assignment.DeepCopy()

	artifactDeployment := &konfidencev1alpha1.ArtifactDeployment{}
	adErr := r.Get(ctx, types.NamespacedName{
		Namespace: assignment.Namespace,
		Name:      assignment.Spec.ArtifactDeploymentRef.Name,
	}, artifactDeployment)
	if adErr != nil && !apierrors.IsNotFound(adErr) {
		return ctrl.Result{}, fmt.Errorf("could not get referenced artifact deployment: %w", adErr)
	}

	status, reason, message := metav1.ConditionFalse, "ArtifactDeploymentMissing", "referenced ArtifactDeployment not found"
	switch {
	case adErr == nil && meta.IsStatusConditionTrue(artifactDeployment.Status.Conditions, konfidencev1alpha1.DeploymentResultCreatedCondition):
		status, reason, message = metav1.ConditionTrue, "AssignmentReady", "ArtifactDeployment published its deployment results"
	case adErr == nil:
		status, reason, message = metav1.ConditionFalse, "AssignmentNotReady", "waiting for ArtifactDeployment to publish deployment results"
	}

	meta.SetStatusCondition(&assignment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.VectorAssignmentReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: assignment.Generation,
		LastTransitionTime: metav1.Now(),
	})

	if reflect.DeepEqual(assignment.Status, original.Status) {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Patch(ctx, assignment, client.MergeFrom(original)); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to patch vector assignment status: %w", err)
	}
	r.Recorder.Eventf(assignment, nil, corev1.EventTypeNormal, "VectorAssignmentReadyChanged", "VectorAssignmentReadyChanged",
		fmt.Sprintf("Ready=%s: %s", status, message))
	return ctrl.Result{}, nil
}

// assignmentsForArtifactDeployment enqueues the VectorAssignments that reference the given ArtifactDeployment so
// their readiness re-evaluates when the ArtifactDeployment's deployment results change.
func (r *VectorAssignmentReconciler) assignmentsForArtifactDeployment(ctx context.Context, obj client.Object) []reconcile.Request {
	deployment, ok := obj.(*konfidencev1alpha1.ArtifactDeployment)
	if !ok {
		return nil
	}

	assignments := &konfidencev1alpha1.VectorAssignmentList{}
	if err := r.List(ctx, assignments, client.InNamespace(deployment.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "listing VectorAssignments for ArtifactDeployment", "artifactDeployment", deployment.Name)
		return nil
	}

	var requests []reconcile.Request
	for i := range assignments.Items {
		assignment := &assignments.Items[i]
		if assignment.Spec.ArtifactDeploymentRef.Name == deployment.Name {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: assignment.Namespace, Name: assignment.Name},
			})
		}
	}
	return requests
}
