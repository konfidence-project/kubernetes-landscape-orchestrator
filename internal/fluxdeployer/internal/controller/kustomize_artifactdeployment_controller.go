package controller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	// see https://github.com/fluxcd/kustomize-controller/tree/main/api/v1
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"

	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
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
	deployment := &konfidencev1alpha1.ArtifactDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get artifact deployment object: %w", err)
	}
	if deployment.Spec.Manifest.Type != internal.DeploymentClassKustomize {
		return ctrl.Result{}, nil
	}
	active, err := deploymentClassActive(ctx, r.Client, deployment.Spec.Manifest.Type)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !active {
		return ctrl.Result{}, nil
	}

	log := logf.FromContext(ctx)
	log.Info("start reconciling Kustomize artifact deployment")

	// preserve original deployment status for patching it later
	originalDeployment := deployment.DeepCopy()
	patch := client.MergeFrom(originalDeployment)

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
						log.Error(err, fmt.Sprintf("failed to reconcile Kustomization of OCM resource '%s'", ocmResource.Name),
							"ArtifactDeployment", deployment)
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

	err = r.ReadyConditionStatusUpdater.MutateStatus(ctx, deployment)
	if err != nil {
		log.Error(err, "failed to mutate status condition to READY ", "ArtifactDeployment", deployment)
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
	kustomizeDeployment := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		deployment, ok := obj.(*konfidencev1alpha1.ArtifactDeployment)
		return ok && deployment.Spec.Manifest.Type == internal.DeploymentClassKustomize
	})
	kustomizeTarget := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		target, ok := obj.(*konfidencev1alpha1.DeploymentTarget)
		return ok && target.Spec.DeploymentClassName == internal.DeploymentClassKustomize
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.ArtifactDeployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}, kustomizeDeployment)).
		Watches(&konfidencev1alpha1.DeploymentTarget{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []ctrl.Request {
			return artifactDeploymentsForTarget(ctx, r.Client, obj.(*konfidencev1alpha1.DeploymentTarget))
		}), builder.WithPredicates(kustomizeTarget)).
		Watches(&konfidencev1alpha1.DeploymentClass{}, deploymentClassEventHandler(r.Client, internal.DeploymentClassKustomize)).
		Owns(&sourcev1.OCIRepository{}).
		Owns(&kustomizev1.Kustomization{}).
		Named("kustomize_artifactdeployment").
		Complete(r)
}
