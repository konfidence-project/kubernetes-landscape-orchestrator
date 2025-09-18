/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package fluxcd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	meta2 "github.com/fluxcd/pkg/apis/meta"
)

// FluxConfigProvider defines the interface for providing Flux specific configuration for ArtifactDeployments
type FluxConfigProvider interface {

	// GetReconcileInterval retrieves the reconcile interval for the landscape
	GetReconcileInterval(landscape string) metav1.Duration

	// GetHelmInstallConfig retrieves the installation configuration for Helm charts for the landscape
	GetHelmInstallConfig(landscape string) *helmv2.Install

	// GetHelmDriftDetectionMode retrieves the drift detection mode for Helm charts for the landscape
	GetHelmDriftDetectionMode(landscape string) *helmv2.DriftDetection

	// GetKubeConfigRef retrieves the kubeconfig for the target cluster of the landscape
	GetKubeConfigRef(landscape string) *meta2.KubeConfigReference

	// GetTargetNamespace retrieves the target namespace of the landscape
	GetTargetNamespace(landscape string) string
}
