package controller

import (
	"context"
	"fmt"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/reconciler"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deployer"
)

// HelmArtifactDeploymentReconciler reconciles ArtifactDeployment objects where manifest type is 'Helm'
type HelmArtifactDeploymentReconciler struct {
	client.Client

	ConfigProvider fluxcd.FluxConfigProvider

	HelmRepositoryReconciler *reconciler.HelmRepositoryReconciler
	HelmReleaseReconciler    *reconciler.HelmReleaseReconciler
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *HelmArtifactDeploymentReconciler) Reconcile(ctx context.Context, ad *konfidencev1alpha1.ArtifactDeployment) (deployer.ReconcileResult, error) {
	// TODO karsten: optimize how deployment targets are resolved here, errors should basically not happen
	kubeConfig, err := r.ConfigProvider.GetKubeConfigRef(ctx, ad.Namespace, ad.Spec.Manifest.Type)
	if err != nil {
		return deployer.ReconcileResult{}, fmt.Errorf("failed to get kubeconfig for ArtifactDeployment %s/%s: %w", ad.Namespace, ad.Name, err)
	}

	resource, err := singleOCMResource(ad.Spec.Component, ocmResourceTypeHelmChart)
	if err != nil {
		condition := metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ArtifactDeploymentReasonInvalidConstraint,
			Message:            err.Error(),
			ObservedGeneration: ad.Generation,
			LastTransitionTime: metav1.Now(),
		}
		return deployer.ReconcileResult{Conditions: []metav1.Condition{condition}}, nil
	}

	helmResource, err := fluxcd.Map(*resource).ToHelm()
	if err != nil {
		condition := metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ArtifactDeploymentReasonInvalidConstraint,
			Message:            err.Error(),
			ObservedGeneration: ad.Generation,
			LastTransitionTime: metav1.Now(),
		}
		return deployer.ReconcileResult{Conditions: []metav1.Condition{condition}}, nil
	}

	// TODO karsten:

	if _, err := r.HelmRepositoryReconciler.Reconcile(ctx, ad, helmResource); err != nil {
		return fmt.Errorf("reconcile HelmRepository: %w", err)
	}
	if _, err := r.HelmReleaseReconciler.Reconcile(ctx, ad, helmResource, kubeConfig); err != nil {
		return fmt.Errorf("reconcile HelmRelease: %w", err)
	}

	// TODO karsten: insert deployment result status updater logic

	return nil
}

func singleOCMResource(component konfidencev1alpha1.OCMComponent, resourceType string) (*konfidencev1alpha1.OCMResource, error) {
	var match *konfidencev1alpha1.OCMResource
	count := 0
	for i := range component.Resources {
		if component.Resources[i].Type == resourceType {
			count++
			match = &component.Resources[i]
		}
	}
	if count == 1 {
		return match, nil
	}

	return nil, fmt.Errorf("expected exactly one OCM resource of type %q (found %d); refusing to reconcile", ocmResourceTypeHelmChart, count)
}

// TODO karsten: move to proper place
const ArtifactDeploymentReasonInvalidConstraint = "ArtifactDeploymentInvalidConstraint"
