package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/reconciler"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deployer"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//go:generate go run go.uber.org/mock/mockgen -destination=./mocks/mock_artifact_deployer.go -package=mocks github.com/konfidence-project/konfidence/pkg/deployer ArtifactDeployer

var _ deployer.ArtifactDeployer = (*HelmArtifactDeployer)(nil)
var _ deployer.ArtifactDeployer = (*KustomizeArtifactDeployer)(nil)

// HelmArtifactDeployer deploys Helm artifacts through Flux.
type HelmArtifactDeployer struct {
	DeploymentTargetResolver DeploymentTargetResolver
	HelmRepositoryReconciler *reconciler.HelmRepositoryReconciler
	HelmReleaseReconciler    *reconciler.HelmReleaseReconciler
}

func (d *HelmArtifactDeployer) Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) error {
	kubeConfig, err := d.DeploymentTargetResolver.GetKubeConfigRef(ctx, deployment.Namespace, deployment.Spec.Manifest.Type)
	if err != nil {
		return err
	}

	resource, err := singleOCMResource(deployment, ocmResourceTypeHelmChart, "MultipleHelmChartResources")
	if err != nil || resource == nil {
		return err
	}
	helmResource, err := fluxcd.Map(*resource).ToHelm()
	if err != nil {
		return fmt.Errorf("map OCM resource %q to HelmChartResource: %w", resource.Name, err)
	}
	if _, err := d.HelmRepositoryReconciler.Reconcile(ctx, deployment, helmResource); err != nil {
		return fmt.Errorf("reconcile HelmRepository: %w", err)
	}
	if _, err := d.HelmReleaseReconciler.Reconcile(ctx, deployment, helmResource, kubeConfig); err != nil {
		return fmt.Errorf("reconcile HelmRelease: %w", err)
	}
	return nil
}

// KustomizeArtifactDeployer deploys Kustomize artifacts through Flux.
type KustomizeArtifactDeployer struct {
	DeploymentTargetResolver DeploymentTargetResolver
	OCIRepositoryReconciler  *reconciler.OCIRepositoryReconciler
	KustomizationReconciler  *reconciler.KustomizationReconciler
}

func (d *KustomizeArtifactDeployer) Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) error {
	kubeConfig, err := d.DeploymentTargetResolver.GetKubeConfigRef(ctx, deployment.Namespace, deployment.Spec.Manifest.Type)
	if err != nil {
		return err
	}

	resource, err := singleOCMResource(deployment, ocmResourceTypeKustomize, "MultipleKustomizeResources")
	if err != nil || resource == nil {
		return err
	}
	kustomizeResource, err := fluxcd.Map(*resource).ToKustomize()
	if err != nil {
		return fmt.Errorf("map OCM resource %q to KustomizeResource: %w", resource.Name, err)
	}
	ready, err := d.OCIRepositoryReconciler.Reconcile(ctx, deployment, kustomizeResource)
	if err != nil {
		return fmt.Errorf("reconcile OCIRepository: %w", err)
	}
	if !ready {
		return nil
	}
	if _, err := d.KustomizationReconciler.Reconcile(ctx, deployment, kustomizeResource, kubeConfig); err != nil {
		return fmt.Errorf("reconcile Kustomization: %w", err)
	}
	return nil
}

func singleOCMResource(deployment *konfidencev1alpha1.ArtifactDeployment, resourceType, reason string) (*konfidencev1alpha1.OCMResource, error) {
	var match *konfidencev1alpha1.OCMResource
	count := 0
	for i := range deployment.Spec.Component.Resources {
		if deployment.Spec.Component.Resources[i].Type == resourceType {
			count++
			match = &deployment.Spec.Component.Resources[i]
		}
	}
	if count <= 1 {
		return match, nil
	}

	message := fmt.Sprintf("expected exactly one OCM resource of type %q, found %d; refusing to reconcile", resourceType, count)
	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: deployment.Generation,
		LastTransitionTime: metav1.Now(),
	})
	return nil, fmt.Errorf("%s", message)
}
