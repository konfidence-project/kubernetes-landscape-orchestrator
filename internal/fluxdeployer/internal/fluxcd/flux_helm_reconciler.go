package fluxcd

import (
	"context"

	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
)

// FluxHelmReconciler defines the interface for reconciling helm related types of Flux resources
// based on ArtifactDeployment specifications.
type FluxHelmReconciler interface {

	// Reconcile creates or updates the appropriate Flux resources (HelmRelease, etc.)
	// based on the provided ArtifactDeployment and OCMResource.
	Reconcile(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, helmChartResource *HelmChartResource) (isReady bool, err error)
}

type HelmChartResource struct {
	landscapev1alpha1.OCMResource
	Repository string
	ChartName  string
	Version    string
}
