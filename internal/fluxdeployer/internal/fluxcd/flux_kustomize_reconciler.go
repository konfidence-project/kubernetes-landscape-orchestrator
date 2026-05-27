package fluxcd

import (
	"context"

	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
)

// FluxKustomizeReconciler defines the interface for reconciling kustomize related types of Flux resources
// based on ArtifactDeployment specifications.
type FluxKustomizeReconciler interface {

	// Reconcile creates or updates the appropriate Flux resources (Kustomization, etc.)
	// based on the provided ArtifactDeployment and OCMResource.
	Reconcile(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, kustomizeResource *KustomizeResource) (isReady bool, err error)
}

type KustomizeResource struct {
	landscapev1alpha1.OCMResource
	Repository string
	Path       string
	Tag        string
}
