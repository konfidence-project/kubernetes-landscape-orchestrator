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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	// see https://github.com/fluxcd/helm-controller/tree/main/api/v2
	helmv2 "github.com/fluxcd/helm-controller/api/v2"

	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
)

// HelmArtifactDeploymentReconciler reconciles ArtifactDeployment objects where manifest type is 'Helm'
type HelmArtifactDeploymentReconciler struct {
	client.Client
	DeploymentResultStatusUpdater StatusUpdater
	ReadyConditionStatusUpdater   StatusUpdater
	HelmRepositoryReconciler      fluxcd.FluxHelmReconciler
	HelmReleaseReconciler         fluxcd.FluxHelmReconciler
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *HelmArtifactDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("start reconciling Helm artifact deployment")

	// get the ArtifactDeployment object
	deployment := &konfidencev1alpha1.ArtifactDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get artifact deployment object: %w", err)
	}

	// preserve original deployment status for patching it later
	originalDeployment := deployment.DeepCopy()
	patch := client.MergeFrom(originalDeployment)

	// reconcile the single OCM resource of type "helmChart"; reject spec with multiple matches
	var matches []konfidencev1alpha1.OCMResource
	for _, ocmResource := range deployment.Spec.Component.Resources {
		if ocmResource.Type == ocmResourceTypeHelmChart {
			matches = append(matches, ocmResource)
		}
	}

	if len(matches) > 1 {
		msg := fmt.Sprintf("expected exactly one OCM resource of type %q, found %d; refusing to reconcile", ocmResourceTypeHelmChart, len(matches))
		meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             "MultipleHelmChartResources",
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
		helmChartResource, err := fluxcd.Map(ocmResource).ToHelm()
		if err != nil {
			log.Error(err, fmt.Sprintf("failed to map OCM resource %q to HelmChartResource", ocmResource.Name),
				"ArtifactDeployment", deployment)
		} else {
			if _, err := r.HelmRepositoryReconciler.Reconcile(ctx, deployment, helmChartResource); err != nil {
				log.Error(err, fmt.Sprintf("failed to reconcile HelmRepository of OCM resource %q", ocmResource.Name),
					"ArtifactDeployment", deployment)
			} else {
				if _, err := r.HelmReleaseReconciler.Reconcile(ctx, deployment, helmChartResource); err != nil {
					log.Error(err, fmt.Sprintf("failed to reconcile HelmRelease of OCM resource %s", ocmResource.Name),
						"ArtifactDeployment", deployment)
				}
			}
		}
	}

	err := r.DeploymentResultStatusUpdater.MutateStatus(ctx, deployment)
	if err != nil {
		log.Error(err, "failed to handle Helm artifact deployment result", "ArtifactDeployment", deployment)
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

	log.Info("finish reconciling Helm artifact deployment")
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HelmArtifactDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Create a predicate to filter ...
	manifestTypeFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch obj := obj.(type) {
		case *konfidencev1alpha1.ArtifactDeployment:
			// ... for 'Helm' manifest types
			return obj.Spec.Manifest.Type == manifestTypeHelm
		case *sourcev1.HelmRepository, *sourcev1.HelmChart, *helmv2.HelmRelease:
			// ... or owned resources
			return true
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.ArtifactDeployment{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(manifestTypeFilter).
		Owns(&sourcev1.HelmRepository{}).
		Owns(&sourcev1.HelmChart{}).
		Owns(&helmv2.HelmRelease{}).
		Named("helm_artifactdeployment").
		Complete(r)
}
