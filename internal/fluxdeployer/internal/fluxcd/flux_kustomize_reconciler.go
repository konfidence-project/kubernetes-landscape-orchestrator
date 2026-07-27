package fluxcd

import (
	"context"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
)

// FluxKustomizeReconciler defines the interface for reconciling kustomize related types of Flux resources
// based on ArtifactDeployment specifications.
type FluxKustomizeReconciler interface {

	// Reconcile creates or updates the appropriate Flux resources (Kustomization, etc.)
	// based on the provided ArtifactDeployment and OCMResource.
	Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment, kustomizeResource *KustomizeResource) (isReady bool, err error)
}

type KustomizeResource struct {
	konfidencev1alpha1.OCMResource
	Repository string
	Path       string
	Tag        string
}
