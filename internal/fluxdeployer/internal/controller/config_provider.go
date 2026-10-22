package controller

import (
	"context"
	"fmt"
	"time"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	targethelpers "github.com/konfidence-project/konfidence/pkg/deployer/deployment-target"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"

	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
)

type ConfigProvider struct {
	Client client.Client
}

var _ fluxcd.FluxConfigProvider = (*ConfigProvider)(nil)

func (r *ConfigProvider) GetReconcileInterval(landscape string) metav1.Duration {
	return metav1.Duration{Duration: 10 * time.Second}
}

func (r *ConfigProvider) GetHelmInstallConfig(landscape string) *helmv2.Install {
	return &helmv2.Install{
		Timeout: &metav1.Duration{Duration: 5 * time.Minute},
	}
}

func (r *ConfigProvider) GetHelmDriftDetectionMode(landscape string) *helmv2.DriftDetection {
	return &helmv2.DriftDetection{
		Mode: helmv2.DriftDetectionEnabled,
	}
}

func (r *ConfigProvider) GetKubeConfigRef(ctx context.Context, landscape, artifactType string) (*fluxmeta.KubeConfigReference, error) {
	logger := crlog.Log.WithName("ConfigProvider").WithValues("landscape", landscape, "artifactType", artifactType)
	target, err := targethelpers.FindReady(ctx, r.Client, landscape, konfidencev1alpha1.ArtifactType(artifactType))
	if err != nil {
		return nil, err
	}
	params := target.Spec.Parameters
	if params == nil {
		logger.Info("using local DeploymentTarget", "name", target.Name)
		return nil, nil
	}
	if params.APIGroup != "" || params.Kind != "Secret" || params.Name == "" {
		return nil, fmt.Errorf("DeploymentTarget %q has invalid KLO kubeconfig parameters", target.Name)
	}

	logger.Info("using remote DeploymentTarget", "name", target.Name, "secret", params.Name)
	return &fluxmeta.KubeConfigReference{
		SecretRef: &fluxmeta.SecretKeyReference{Name: params.Name},
	}, nil
}

func (r *ConfigProvider) GetTargetNamespace(landscape string) string {
	return landscape
}
