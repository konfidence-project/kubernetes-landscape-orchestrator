package fluxcd

//go:generate go run go.uber.org/mock/mockgen -destination=./mocks/mock_flux_helm_reconciler.go -package=mocks github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd FluxHelmReconciler,FluxHelmWorkloadReconciler

import (
	"context"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
)

// FluxHelmReconciler defines the interface for reconciling helm related types of Flux resources
// based on ArtifactDeployment specifications.
type FluxHelmReconciler interface {

	// Reconcile creates or updates the appropriate Flux resources (HelmRelease, etc.)
	// based on the provided ArtifactDeployment and OCMResource.
	Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment, helmChartResource *HelmChartResource) (isReady bool, err error)
}

// FluxHelmWorkloadReconciler reconciles workload resources after target resolution.
type FluxHelmWorkloadReconciler interface {
	Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment, helmChartResource *HelmChartResource,
		kubeConfig *fluxmeta.KubeConfigReference) (isReady bool, err error)
}

type HelmChartResource struct {
	konfidencev1alpha1.OCMResource
	Repository string
	ChartName  string
	Version    string
}
