package controller

import (
	"context"
	"fmt"
	"time"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"

	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
)

// ConfigProvider resolves Flux configuration for a given landscape namespace.
// It queries active DeploymentClasses from the informer cache to determine which
// spec.type values are owned by this controller, then looks up the matching
// DeploymentTarget in the landscape namespace to determine whether Flux should deploy
// to a remote cluster through a kubeconfig Secret or to its local cluster.
type ConfigProvider struct {
	Client client.Client
}

var _ fluxcd.FluxConfigProvider = (*ConfigProvider)(nil)

const (
	connectionTypeKubeconfig = "kubeconfig"
	connectionTypeLocal      = "local"
)

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

// GetKubeConfigRef resolves the kubeconfig Secret reference for the given landscape
// namespace. It lists DeploymentTargets in that namespace and returns the first one
// whose spec.type is covered by an active DeploymentClass owned by this controller.
// Returns nil if no matching DeploymentTarget is found (local-cluster deployment).
func (r *ConfigProvider) GetKubeConfigRef(ctx context.Context, landscape string) (*fluxmeta.KubeConfigReference, error) {
	logger := crlog.Log.WithName("ConfigProvider").WithValues("landscape", landscape)

	activeTypes, err := deploymentclass.ActiveTypes(ctx, r.Client)
	if err != nil {
		return nil, fmt.Errorf("resolve active deployment class types: %w", err)
	}

	list := &konfidencev1alpha1.DeploymentTargetList{}
	if err := r.Client.List(ctx, list, client.InNamespace(landscape)); err != nil {
		return nil, fmt.Errorf("list DeploymentTargets in namespace %q: %w", landscape, err)
	}

	for i := range list.Items {
		dt := &list.Items[i]

		if _, active := activeTypes[dt.Spec.Type]; !active {
			continue
		}

		if dt.Spec.Connection.Type == connectionTypeLocal {
			logger.Info("found local DeploymentTarget", "name", dt.Name, "type", dt.Spec.Type)
			return nil, nil
		}

		if dt.Spec.Connection.Type != connectionTypeKubeconfig {
			logger.Info("skipping DeploymentTarget with unsupported connection type",
				"name", dt.Name, "connectionType", dt.Spec.Connection.Type)
			continue
		}

		// Only Secret refs are supported for kubeconfig connections.
		if dt.Spec.Connection.Ref == nil || dt.Spec.Connection.Ref.Kind != "Secret" {
			kind := ""
			if dt.Spec.Connection.Ref != nil {
				kind = dt.Spec.Connection.Ref.Kind
			}
			logger.Info("skipping DeploymentTarget with unsupported connection ref kind",
				"name", dt.Name, "kind", kind)
			continue
		}

		logger.Info("found DeploymentTarget", "name", dt.Name, "type", dt.Spec.Type,
			"secret", dt.Spec.Connection.Ref.Name)

		return &fluxmeta.KubeConfigReference{
			SecretRef: &fluxmeta.SecretKeyReference{
				Name: dt.Spec.Connection.Ref.Name,
				// Key is intentionally omitted so Flux uses its default key ("value" or "value.yaml").
				// See https://fluxcd.io/flux/components/kustomize/kustomizations/#secret-based-authentication
			},
		}, nil
	}

	// No matching DeploymentTarget found: deploy to the local cluster.
	logger.Info("no DeploymentTarget found, deploying to local cluster")
	return nil, nil
}

func (r *ConfigProvider) GetTargetNamespace(landscape string) string {
	return landscape
}
