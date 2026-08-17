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

// ConfigProvider resolves Flux configuration for a given landscape namespace.
// It queries active DeploymentClasses from the informer cache to determine which
// spec.type values are owned by this controller, then looks up the matching
// DeploymentTarget in the landscape namespace to determine whether Flux should deploy
// to a remote cluster through a kubeconfig Secret or to its local cluster.
type ConfigProvider struct {
	Client client.Reader
}

var _ fluxcd.FluxConfigProvider = (*ConfigProvider)(nil)

const (
	// ReadyDeploymentTargetTypeField indexes ready DeploymentTargets by spec.type.
	ReadyDeploymentTargetTypeField = "konfidence.cloud/ready-deployment-target-type"

	connectionTypeLocal = "local"
)

// DeploymentTargetNotReadyError indicates that no ready target exists for a deployment type.
type DeploymentTargetNotReadyError struct {
	Namespace      string
	DeploymentType string
}

func (e *DeploymentTargetNotReadyError) Error() string {
	return fmt.Sprintf("no ready DeploymentTarget for type %q found in namespace %q", e.DeploymentType, e.Namespace)
}

// IndexReadyDeploymentTargets registers the cache index used by GetKubeConfigRef.
func IndexReadyDeploymentTargets(ctx context.Context, indexer client.FieldIndexer) error {
	return indexer.IndexField(ctx, &konfidencev1alpha1.DeploymentTarget{}, ReadyDeploymentTargetTypeField,
		readyDeploymentTargetTypeIndex)
}

func readyDeploymentTargetTypeIndex(obj client.Object) []string {
	dt := obj.(*konfidencev1alpha1.DeploymentTarget)
	ready := meta.FindStatusCondition(dt.Status.Conditions, konfidencev1alpha1.DeploymentTargetReadyCondition)
	if ready == nil || ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != dt.Generation {
		return nil
	}
	return []string{dt.Spec.Type}
}

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
// namespace and deployment type. Only targets with a Ready=True condition are returned
// by the cache index. A local target is represented by a nil kubeconfig reference.
func (r *ConfigProvider) GetKubeConfigRef(ctx context.Context, landscape, deploymentType string) (*fluxmeta.KubeConfigReference, error) {
	logger := crlog.Log.WithName("ConfigProvider").WithValues("landscape", landscape)

	list := &konfidencev1alpha1.DeploymentTargetList{}
	if err := r.Client.List(ctx, list,
		client.InNamespace(landscape),
		client.MatchingFields{ReadyDeploymentTargetTypeField: deploymentType},
	); err != nil {
		return nil, fmt.Errorf("list DeploymentTargets in namespace %q: %w", landscape, err)
	}

	for i := range list.Items {
		dt := &list.Items[i]

		if dt.Spec.Connection.Type == connectionTypeLocal {
			logger.Info("found local DeploymentTarget", "name", dt.Name, "type", dt.Spec.Type)
			return nil, nil
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

	return nil, &DeploymentTargetNotReadyError{Namespace: landscape, DeploymentType: deploymentType}
}

func (r *ConfigProvider) GetTargetNamespace(landscape string) string {
	return landscape
}
