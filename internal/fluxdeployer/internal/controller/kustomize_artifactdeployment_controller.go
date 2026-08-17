package controller

import (
	"context"
	"fmt"
	"reflect"

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

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	// see https://github.com/fluxcd/kustomize-controller/tree/main/api/v1
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"

	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
)

// KustomizeArtifactDeploymentReconciler reconciles ArtifactDeployment objects where manifest type is 'Kustomize'
type KustomizeArtifactDeploymentReconciler struct {
	client.Client
	DeploymentResultStatusUpdater StatusUpdater
	ReadyConditionStatusUpdater   StatusUpdater
	OCIRepositoryReconciler       fluxcd.FluxKustomizeReconciler
	KustomizationReconciler       fluxcd.FluxKustomizeReconciler
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=ocirepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kustomize.toolkit.fluxcd.io,resources=kustomizations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *KustomizeArtifactDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("start reconciling Kustomize artifact deployment")

	// get the ArtifactDeployment object
	deployment := &konfidencev1alpha1.ArtifactDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get artifact deployment object: %w", err)
	}

	// Only reconcile ArtifactDeployments whose manifest type is currently covered by
	// an active DeploymentClass we own. This check uses the informer cache.
	activeTypes, err := deploymentclass.ActiveTypes(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve active deployment class types: %w", err)
	}
	if _, active := activeTypes[deployment.Spec.Manifest.Type]; !active {
		return ctrl.Result{}, nil
	}

	// preserve original deployment status for patching it later
	originalDeployment := deployment.DeepCopy()
	patch := client.MergeFrom(originalDeployment)
	deploymentTargetNotReady := false

	// reconcile the single OCM resource of type "kustomize"; reject spec with multiple matches
	var matches []konfidencev1alpha1.OCMResource
	for _, ocmResource := range deployment.Spec.Component.Resources {
		if ocmResource.Type == ocmResourceTypeKustomize {
			matches = append(matches, ocmResource)
		}
	}

	if len(matches) > 1 {
		msg := fmt.Sprintf("expected exactly one OCM resource of type %q, found %d; refusing to reconcile", ocmResourceTypeKustomize, len(matches))
		meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "MultipleKustomizeResources",
			Message:            msg,
			ObservedGeneration: deployment.Generation,
			LastTransitionTime: metav1.Now(),
		})

		if !reflect.DeepEqual(deployment.Status, originalDeployment.Status) {
			if err := r.Client.Status().Patch(ctx, deployment, patch); err != nil {
				return ctrl.Result{}, fmt.Errorf("unable to patch artifact deployment status: %w", err)
			}
		}

		return ctrl.Result{}, fmt.Errorf("%s", msg)
	}

	if len(matches) == 1 {
		ocmResource := matches[0]
		kustomizeResource, err := fluxcd.Map(ocmResource).ToKustomize()
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to map OCM resource %q to KustomizeResource", ocmResource.Name),
				"ArtifactDeployment", deployment)
		} else {
			if isReady, err := r.OCIRepositoryReconciler.Reconcile(ctx, deployment, kustomizeResource); err != nil {
				log.Error(err, fmt.Sprintf("failed to reconcile OCIRepository of OCM resource '%s'", ocmResource.Name),
					"ArtifactDeployment", deployment)
			} else {
				if isReady {
					if _, err := r.KustomizationReconciler.Reconcile(ctx, deployment, kustomizeResource); err != nil {
						deploymentTargetNotReady = setDeploymentTargetNotReady(deployment, err)
						if !deploymentTargetNotReady {
							log.Error(err, fmt.Sprintf("failed to reconcile Kustomization of OCM resource '%s'", ocmResource.Name),
								"ArtifactDeployment", deployment)
						}
					}
				} else {
					log.Info("OCIRepository is not ready, skipping Kustomization reconciliation")
				}
			}
		}
	}

	err = r.DeploymentResultStatusUpdater.MutateStatus(ctx, deployment)
	if err != nil {
		log.Error(err, "failed to handle Kustomize deployment result", "ArtifactDeployment", deployment)
	}

	if !deploymentTargetNotReady {
		err = r.ReadyConditionStatusUpdater.MutateStatus(ctx, deployment)
		if err != nil {
			log.Error(err, "failed to mutate status condition to READY ", "ArtifactDeployment", deployment)
		}
	}

	// patch the deployment status updates
	if !reflect.DeepEqual(deployment.Status, originalDeployment.Status) {
		if err := r.Client.Status().Patch(ctx, deployment, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to patch artifact deployment status: %w", err)
		}
	}

	log.Info("finish reconciling Kustomize artifact deployment")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KustomizeArtifactDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a predicate to filter ...
	manifestTypeFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *konfidencev1alpha1.ArtifactDeployment:
			// ... for 'Kustomize' manifest types
			return obj.Spec.Manifest.Type == manifestTypeKustomize
		case *sourcev1.OCIRepository, *kustomizev1.Kustomization:
			// ... or owned resources
			return true
		case *konfidencev1alpha1.DeploymentTarget:
			return obj.Spec.Type == manifestTypeKustomize
		default:
			return false
		}
	})

	// deploymentClassMapper re-enqueues all ArtifactDeployments of the kustomize type when
	// the corresponding DeploymentClass is created or deleted, so the active-type check
	// in Reconcile reflects the current state without waiting for the next spec change.
	deploymentClassMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			dc, ok := obj.(*konfidencev1alpha1.DeploymentClass)
			if !ok || dc.Spec.Controller != deploymentclass.ControllerName || dc.Spec.Type != manifestTypeKustomize {
				return nil
			}
			return deploymentclass.ArtifactDeploymentsForType(ctx, r.Client, manifestTypeKustomize)
		},
	)
	deploymentTargetMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			dt, ok := obj.(*konfidencev1alpha1.DeploymentTarget)
			if !ok || dt.Spec.Type != manifestTypeKustomize {
				return nil
			}
			return deploymentclass.ArtifactDeploymentsForTarget(ctx, r.Client, dt)
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.ArtifactDeployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(manifestTypeFilter).
		Owns(&sourcev1.OCIRepository{}).
		Owns(&kustomizev1.Kustomization{}).
		Watches(&konfidencev1alpha1.DeploymentClass{}, deploymentClassMapper).
		Watches(&konfidencev1alpha1.DeploymentTarget{}, deploymentTargetMapper).
		Named("kustomize_artifactdeployment").
		Complete(r)
}
