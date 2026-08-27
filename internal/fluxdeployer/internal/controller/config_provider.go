package controller

import (
	"context"
	"fmt"
	"time"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
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

const connectionTypeLocal = "local"

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

func (r *ConfigProvider) GetKubeConfigRef(ctx context.Context, landscape, deploymentType string) (*fluxmeta.KubeConfigReference, error) {
	logger := crlog.Log.WithName("ConfigProvider").WithValues("landscape", landscape, "deploymentType", deploymentType)
	targets := &konfidencev1alpha1.DeploymentTargetList{}
	if err := r.Client.List(ctx, targets, client.InNamespace(landscape)); err != nil {
		return nil, fmt.Errorf("list DeploymentTargets in namespace %q: %w", landscape, err)
	}

	var readyTargets []*konfidencev1alpha1.DeploymentTarget
	for i := range targets.Items {
		target := &targets.Items[i]
		if target.Spec.DeploymentClassName != deploymentType {
			continue
		}

		if meta.IsStatusConditionTrue(target.Status.Conditions, konfidencev1alpha1.DeploymentTargetReadyCondition) {
			readyTargets = append(readyTargets, target)
		}
	}

	if len(readyTargets) != 1 {
		return nil, fmt.Errorf(
			"expected exactly one ready DeploymentTarget in namespace %q for DeploymentClass %q, found %d",
			landscape,
			deploymentType,
			len(readyTargets),
		)
	}

	target := readyTargets[0]
	if target.Spec.Connection.Type == connectionTypeLocal {
		logger.Info("using local DeploymentTarget", "name", target.Name)
		return nil, nil
	}

	logger.Info("using remote DeploymentTarget", "name", target.Name, "secret", target.Spec.Connection.Ref.Name)
	return &fluxmeta.KubeConfigReference{
		SecretRef: &fluxmeta.SecretKeyReference{Name: target.Spec.Connection.Ref.Name},
	}, nil
}

func (r *ConfigProvider) GetTargetNamespace(landscape string) string {
	return landscape
}
