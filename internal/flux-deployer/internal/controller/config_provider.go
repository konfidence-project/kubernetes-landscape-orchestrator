package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/konfidence-project/landscape-flux-deployer/internal/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"

	"sigs.k8s.io/controller-runtime/pkg/client"
	crlog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd"
)

type ConfigProvider struct {
	Client client.Client
}

type DeploymentTarget struct {
	Landscape    string                         `json:"landscape"`
	SecretRef    *fluxmeta.SecretKeyReference   `json:"secretRef"`
	ConfigMapRef *fluxmeta.LocalObjectReference `json:"configMapRef"`
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

func (r *ConfigProvider) GetKubeConfigRef(ctx context.Context, landscape string) (*fluxmeta.KubeConfigReference, error) {
	logger := crlog.Log.WithName("ConfigProvider").WithValues("landscape", landscape)

	// 1. get deployment targets from configmap (most specific)
	deploymentTarget, err := r.readKubeConfigReferenceFromConfigMap(ctx, landscape)
	if err != nil {
		return nil, err
	}
	if deploymentTarget != nil {
		logger.Info(fmt.Sprintf("found remote deployment target in configmap for landscape %s", landscape))
		return deploymentTarget, nil
	}

	// 2. if no TargetDeployment found in configmap, check if secret in landscape namespace exists (convention)
	deploymentTarget, err = r.readKubeConfigReferenceFromNamespace(ctx, landscape)
	if err != nil {
		return nil, err
	}
	if deploymentTarget != nil {
		logger.Info(fmt.Sprintf("found remote deployment target in namespace secret for landscape %s", landscape))
		return deploymentTarget, nil
	}

	// 3. no remote deployment target found
	logger.Info("no remote deployment target found")
	return nil, nil
}

// readKubeConfigReferenceFromConfigMap reads the deployment target for the landscape from the global ConfigMap in konfidence-system namespace
func (r *ConfigProvider) readKubeConfigReferenceFromConfigMap(ctx context.Context, landscape string) (*fluxmeta.KubeConfigReference, error) {
	cm := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: "konfidence-system", Name: config.DefaultConfigMapName}, cm)
	if err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	// Read key deploymentTargets
	raw, ok := cm.Data["deploymentTargets"]
	if !ok || raw == "" {
		return nil, nil
	}

	var deploymentTargets []DeploymentTarget
	if err := json.Unmarshal([]byte(raw), &deploymentTargets); err != nil {
		return nil, err
	}

	for _, dt := range deploymentTargets {
		if dt.Landscape == landscape {
			// Build KubeConfigReference from matched deployment target
			return &fluxmeta.KubeConfigReference{
				SecretRef:    dt.SecretRef,
				ConfigMapRef: dt.ConfigMapRef,
			}, nil
		}
	}

	return nil, nil
}

func (r *ConfigProvider) readKubeConfigReferenceFromNamespace(ctx context.Context, namespace string) (*fluxmeta.KubeConfigReference, error) {
	const secretName = "konfidence-flux-remote-cluster-kubeconfig"

	secret := &corev1.Secret{}
	err := r.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: secretName}, secret)
	if err != nil {
		return nil, client.IgnoreNotFound(err)
	}

	return &fluxmeta.KubeConfigReference{
		SecretRef: &fluxmeta.SecretKeyReference{
			Name: secretName,
			// don't provide Key here, so flux can use the default key.
			// see https://fluxcd.io/flux/components/kustomize/kustomizations/#secret-based-authentication
		},
	}, nil
}

func (r *ConfigProvider) GetTargetNamespace(landscape string) string {
	return landscape
}
