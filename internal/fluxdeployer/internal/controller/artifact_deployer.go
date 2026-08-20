package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/reconciler"
)

//go:generate go run go.uber.org/mock/mockgen -destination=./mocks/mock_artifact_deployer.go -package=mocks github.com/konfidence-project/konfidence/pkg/deployer ArtifactDeployer

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
