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

package reconciler

import (
	"context"
	"fmt"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/flux-deployer/internal/fluxcd/utils"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// see https://github.com/fluxcd/helm-controller/tree/main/api/v2
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/flux-deployer/internal/fluxcd"
)

//
// Flux HelmRelease docs: https://fluxcd.io/flux/components/helm/helmreleases/
// Flux API reference: https://fluxcd.io/flux/components/helm/api/v2/
//

type HelmReleaseReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
}

var _ fluxcd.FluxHelmReconciler = (*HelmReleaseReconciler)(nil)

func (r *HelmReleaseReconciler) Reconcile(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, helmChartResource *fluxcd.HelmChartResource) (isReady bool, err error) {

	helmRelease := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      buildResourceName(deployment, &helmChartResource.OCMResource),
		},
	}

	mutateFn := func() error { return r.mutateHelmRelease(ctx, deployment, helmChartResource, helmRelease) }

	// create or update the HelmRelease resource
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, helmRelease, mutateFn); err != nil {
		return false, fmt.Errorf("failed to reconcile HelmRelease: %w", err)
	}

	// map the status conditions of the HelmChart and HelmRelease to the ArtifactDeployment
	if helmChart := r.getHelmChart(ctx, deployment, &helmChartResource.OCMResource); helmChart != nil {
		if isReady := r.mapStatusConditionsFromHelmChart(deployment, helmChart); isReady {
			r.mapStatusConditionsFromHelmRelease(deployment, helmRelease)
		} // else: HelmChart is not ready, skipping status update
	} // else: HelmChart not yet available, skipping status update

	return meta.IsStatusConditionTrue(helmRelease.Status.Conditions, conditionTypeReady), nil
}

func (r *HelmReleaseReconciler) mutateHelmRelease(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, helmChartResource *fluxcd.HelmChartResource, helmRelease *helmv2.HelmRelease) error {

	// set owner reference (with controller:=true) if newly created
	if helmRelease.CreationTimestamp.IsZero() {
		if err := controllerutil.SetControllerReference(deployment, helmRelease, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on HelmRelease: %w", err)
		}
	}

	kubeConfig, err := r.ConfigProvider.GetKubeConfigRef(ctx, deployment.GetNamespace())
	if err != nil {
		return err
	}

	// update spec
	helmRelease.Spec = helmv2.HelmReleaseSpec{
		Interval: r.ConfigProvider.GetReconcileInterval(deployment.GetNamespace()),
		Chart: &helmv2.HelmChartTemplate{
			Spec: helmv2.HelmChartTemplateSpec{
				SourceRef: helmv2.CrossNamespaceObjectReference{
					Kind:      sourcev1.HelmRepositoryKind,
					Namespace: deployment.GetNamespace(),
					Name:      buildHelmRepositoryResourceName(deployment, &helmChartResource.OCMResource),
				},
				Chart:   helmChartResource.ChartName,
				Version: helmChartResource.Version,
			},
		},
		KubeConfig:       kubeConfig,
		TargetNamespace:  r.ConfigProvider.GetTargetNamespace(deployment.GetNamespace()),
		StorageNamespace: r.ConfigProvider.GetTargetNamespace(deployment.GetNamespace()),
		DriftDetection:   r.ConfigProvider.GetHelmDriftDetectionMode(deployment.GetNamespace()),
		Install:          r.ConfigProvider.GetHelmInstallConfig(deployment.GetNamespace()),
		CommonMetadata: &helmv2.CommonMetadata{
			Labels: map[string]string{
				"konfidence.cloud/artifact-deployment": utils.SanitizeK8sResourceName(deployment.Name),
			},
		},
	}

	return nil
}

func (r *HelmReleaseReconciler) getHelmChart(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) *sourcev1.HelmChart {

	objectKey := types.NamespacedName{
		Namespace: deployment.GetNamespace(),
		Name:      fmt.Sprintf("%s-%s", deployment.GetNamespace(), buildResourceName(deployment, ocmResource)),
	}
	helmChart := &sourcev1.HelmChart{}

	if err := r.Client.Get(ctx, objectKey, helmChart); err != nil {
		return nil
	}

	return helmChart
}

func (r *HelmReleaseReconciler) mapStatusConditionsFromHelmChart(
	deployment *landscapev1alpha1.ArtifactDeployment, helmChart *sourcev1.HelmChart) bool {

	for _, condition := range helmChart.Status.Conditions {
		if conditionType := mapHelmChartConditionType(condition.Type); conditionType != "" {
			meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
				Type:               conditionType,
				Status:             condition.Status,
				Reason:             condition.Reason,
				Message:            condition.Message,
				ObservedGeneration: deployment.Generation,
				LastTransitionTime: metav1.Now(),
			})
		}
	}

	return meta.IsStatusConditionTrue(helmChart.Status.Conditions, conditionTypeReady)
}

func mapHelmChartConditionType(conditionType string) string {
	switch conditionType {
	case conditionTypeReady:
		return landscapev1alpha1.ArtifactFetchedCondition
	default:
		return ""
	}
}

func (r *HelmReleaseReconciler) mapStatusConditionsFromHelmRelease(
	deployment *landscapev1alpha1.ArtifactDeployment, helmRelease *helmv2.HelmRelease) {

	for _, condition := range helmRelease.Status.Conditions {
		if conditionType := mapHelmReleaseConditionType(condition.Type); conditionType != "" {
			meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
				Type:               conditionType,
				Status:             condition.Status,
				Reason:             condition.Reason,
				Message:            condition.Message,
				ObservedGeneration: deployment.Generation,
				LastTransitionTime: metav1.Now(),
			})
		}
	}
}

func mapHelmReleaseConditionType(conditionType string) string {
	switch conditionType {
	case conditionTypeReady:
		return landscapev1alpha1.ArtifactDeployedCondition
	case "Released":
		return landscapev1alpha1.AppHealthyCondition
	default:
		return ""
	}
}
