package deployer

import (
	"context"
	"fmt"
	"reflect"
	"slices"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ownedResource struct {
	Object  client.Object
	Options []builder.OwnsOption
}

type ArtifactDeploymentReconciler struct {
	client.Client

	controllerName string
	manifestType   string

	ownedResources []ownedResource
	Reconciler     ArtifactReconciler
}

func NewArtifactDeploymentReconciler(client client.Client, controllerName string, manifestType string) *ArtifactDeploymentReconciler {
	return &ArtifactDeploymentReconciler{
		Client:         client,
		controllerName: controllerName,
		manifestType:   manifestType,
	}
}

func (r *ArtifactDeploymentReconciler) Owns(obj client.Object, opts ...builder.OwnsOption) *ArtifactDeploymentReconciler {
	r.ownedResources = append(r.ownedResources, ownedResource{
		Object:  obj,
		Options: opts,
	})
	return r
}

func (r *ArtifactDeploymentReconciler) Complete(reconciler ArtifactReconciler) *ArtifactDeploymentReconciler {
	r.Reconciler = reconciler
	return r
}

func (r *ArtifactDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info(fmt.Sprintf("start reconciling %s artifact deployment", r.manifestType))

	deployment := &konfidencev1alpha1.ArtifactDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get artifact deployment object: %w", err)
	}

	// Only reconcile ArtifactDeployments whose manifest type is
	// currently covered by an active DeploymentClass we own
	activeTypes, err := deploymentclass.ActiveTypes(ctx, r.Client, r.controllerName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to resolve active deployment class types: %w", err)
	}
	if _, active := activeTypes[deployment.Spec.Manifest.Type]; !active {
		return ctrl.Result{}, nil
	}

	// preserve original deployment status for patching it later
	originalDeployment := deployment.DeepCopy()
	patch := client.MergeFrom(originalDeployment)

	// TODO karsten: check if we have a Ready=true deployment target

	result, err := r.Reconciler.Reconcile(ctx, deployment)
	if err != nil {
		meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ArtifactDeploymentReasonReconcileFailed,
			Message:            "controller reconciliation failed",
			ObservedGeneration: deployment.Generation,
			LastTransitionTime: metav1.Now(),
		})
		if !reflect.DeepEqual(deployment.Status, originalDeployment.Status) {
			if patchErr := r.Client.Status().Patch(ctx, deployment, patch); patchErr != nil {
				return ctrl.Result{}, fmt.Errorf("patch ArtifactDeployment status: %w", patchErr)
			}
		}
		return ctrl.Result{}, err
	}

	// add a Ready=False condition if we don't already have one
	if !slices.ContainsFunc(result.Conditions, func(condition metav1.Condition) bool {
		return condition.Type == konfidencev1alpha1.ArtifactDeploymentReadyCondition
	}) {
		result.Conditions = append(result.Conditions, metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: deployment.Generation,
			LastTransitionTime: metav1.Now(),
			Reason:             ArtifactDeploymentReasonNotReady,
			Message:            "reconciler did not set a Ready=True condition, falling back to Ready=False",
		})
	}

	deployment.Status.DeploymentResults = result.DeploymentResults

	if !reflect.DeepEqual(deployment.Status, originalDeployment.Status) {
		if err := r.Client.Status().Patch(ctx, deployment, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to patch artifact deployment status: %w", err)
		}
	}

	log.Info(fmt.Sprintf("finished reconciling %s artifact deployment", r.manifestType))
	return result.Result, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArtifactDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a predicate to filter ...
	manifestTypeFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *konfidencev1alpha1.ArtifactDeployment:
			return obj.Spec.Manifest.Type == r.manifestType
		case *konfidencev1alpha1.DeploymentTarget:
			return obj.Spec.Type == r.manifestType
		default:
			// TODO karsten: check if we actually need this in our filter?
			for _, ownedResource := range r.ownedResources {
				if reflect.TypeOf(obj) == reflect.TypeOf(ownedResource.Object) {
					return true
				}
			}
			return false
		}
	})

	// re-enqueue all ArtifactDeployments of the given manifest type when DeploymentClass
	// or DeploymentTarget is changed, so the active-type check in Reconcile reflects the
	// current state without waiting for the next spec change
	deploymentClassMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			dc, ok := obj.(*konfidencev1alpha1.DeploymentClass)
			if !ok || dc.Spec.Controller != r.controllerName || dc.Spec.Type != r.manifestType {
				return nil
			}
			return deploymentclass.ArtifactDeploymentsForType(ctx, r.Client, r.manifestType)
		},
	)
	deploymentTargetMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			dt, ok := obj.(*konfidencev1alpha1.DeploymentTarget)
			if !ok || dt.Spec.Type != r.manifestType {
				return nil
			}
			return deploymentclass.ArtifactDeploymentsForTarget(ctx, r.Client, dt)
		},
	)

	controller := ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.ArtifactDeployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named(r.controllerName).
		Watches(&konfidencev1alpha1.DeploymentClass{}, deploymentClassMapper).
		Watches(&konfidencev1alpha1.DeploymentTarget{}, deploymentTargetMapper).
		WithEventFilter(manifestTypeFilter)

	for _, ownedResource := range r.ownedResources {
		controller.Owns(ownedResource.Object, ownedResource.Options...)
	}
	return controller.Complete(r)
}

// TODO karsten: move to proper place
const ArtifactDeploymentReasonReconcileFailed = "ArtifactDeploymentReconcileFailed"
const ArtifactDeploymentReasonNotReady = "ArtifactDeploymentNotReady"
