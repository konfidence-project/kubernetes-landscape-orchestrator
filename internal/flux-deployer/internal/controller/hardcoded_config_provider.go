package controller

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	meta2 "github.com/fluxcd/pkg/apis/meta"

	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd"
)

type HardCodedConfigProvider struct{}

var _ fluxcd.FluxConfigProvider = (*HardCodedConfigProvider)(nil)

// TODO (max # 2025-09-15): This is just a dummy implementation. The deployer needs two kind of configurations:
// 1. Konfidence specific configurations, e.g. deployment target.
// 2. Flux specific configurations, e.g. Helm drift detection mode.
// How these configurations are provided to the deployer will be decided with an ADR.

func (r *HardCodedConfigProvider) GetReconcileInterval(landscape string) metav1.Duration {
	return metav1.Duration{Duration: 10 * time.Second}
}

func (r *HardCodedConfigProvider) GetHelmInstallConfig(landscape string) *helmv2.Install {
	return &helmv2.Install{
		Timeout: &metav1.Duration{Duration: 5 * time.Minute},
	}
}

func (r *HardCodedConfigProvider) GetHelmDriftDetectionMode(landscape string) *helmv2.DriftDetection {
	return &helmv2.DriftDetection{
		Mode: helmv2.DriftDetectionEnabled,
	}
}

func (r *HardCodedConfigProvider) GetKubeConfigRef(landscape string) *meta2.KubeConfigReference {
	if landscape == "remote-target-namespace" {
		return &meta2.KubeConfigReference{
			SecretRef: meta2.SecretKeyReference{
				Name: "kubeconfig-remote-cluster",
				Key:  "kubeconfig",
			},
		}
	}

	return nil
}

func (r *HardCodedConfigProvider) GetTargetNamespace(landscape string) string {
	if landscape == "remote-target-namespace" {
		return "konfidence-dev"
	}

	return landscape
}
