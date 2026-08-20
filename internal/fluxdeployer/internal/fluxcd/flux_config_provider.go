package fluxcd

import (
	"context"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
)

// FluxConfigProvider defines the interface for providing Flux specific configuration for ArtifactDeployments
type FluxConfigProvider interface {

	// GetReconcileInterval retrieves the reconcile interval for the landscape
	GetReconcileInterval(landscape string) metav1.Duration

	// GetHelmInstallConfig retrieves the installation configuration for Helm charts for the landscape
	GetHelmInstallConfig(landscape string) *helmv2.Install

	// GetHelmDriftDetectionMode retrieves the drift detection mode for Helm charts for the landscape
	GetHelmDriftDetectionMode(landscape string) *helmv2.DriftDetection

	// GetTargetNamespace retrieves the target namespace of the landscape
	GetTargetNamespace(landscape string) string

	GetKubeConfigRef(ctx context.Context, landscape, deploymentType string) (*fluxmeta.KubeConfigReference, error)
}
