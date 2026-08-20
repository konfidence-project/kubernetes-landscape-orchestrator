package helm

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// see https://github.com/fluxcd/helm-controller/tree/main/api/v2
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
)

//
// Flux HelmRelease docs: https://fluxcd.io/flux/components/helm/helmreleases/
// Flux API reference: https://fluxcd.io/flux/components/helm/api/v2/
//

const conditionTypeReady = "Ready"

type releaseReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
}

func (r *releaseReconciler) Reconcile(
	ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment, helmChartResource *fluxcd.HelmChartResource,
	kubeConfig *fluxmeta.KubeConfigReference) (isReady bool, err error) {

	helmRelease := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      deployment.Name,
		},
	}

	mutateFn := func() error { return r.mutateHelmRelease(deployment, helmChartResource, helmRelease, kubeConfig) }

	// create or update the HelmRelease resource
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, helmRelease, mutateFn); err != nil {
		return false, fmt.Errorf("failed to reconcile HelmRelease: %w", err)
	}

	// TODO karsten: check if we need this condition mapping; if yes, make it work again
	// map the status conditions of the HelmChart and HelmRelease to the ArtifactDeployment
	if helmChart := r.getHelmChart(ctx, deployment); helmChart != nil {
		if isReady := r.mapStatusConditionsFromHelmChart(deployment, helmChart); isReady {
			r.mapStatusConditionsFromHelmRelease(deployment, helmRelease)
		} // else: HelmChart is not ready, skipping status update
	} // else: HelmChart not yet available, skipping status update

	return meta.IsStatusConditionTrue(helmRelease.Status.Conditions, conditionTypeReady), nil
}

func (r *releaseReconciler) mutateHelmRelease(
	deployment *konfidencev1alpha1.ArtifactDeployment,
	helmChartResource *fluxcd.HelmChartResource,
	helmRelease *helmv2.HelmRelease,
	kubeConfig *fluxmeta.KubeConfigReference,
) error {

	// set owner reference (with controller:=true) if newly created
	if helmRelease.CreationTimestamp.IsZero() {
		if err := controllerutil.SetControllerReference(deployment, helmRelease, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on HelmRelease: %w", err)
		}
	}

	// update spec
	helmRelease.Spec = helmv2.HelmReleaseSpec{
		Interval: r.ConfigProvider.GetReconcileInterval(deployment.GetNamespace()),
		Chart: &helmv2.HelmChartTemplate{
			Spec: helmv2.HelmChartTemplateSpec{
				SourceRef: helmv2.CrossNamespaceObjectReference{
					Kind:      sourcev1.HelmRepositoryKind,
					Namespace: deployment.GetNamespace(),
					Name:      deployment.Name,
				},
				Chart:   helmChartResource.ChartName,
				Version: helmChartResource.Version,
			},
		},
		ReleaseName:      deployment.Name,
		KubeConfig:       kubeConfig,
		TargetNamespace:  r.ConfigProvider.GetTargetNamespace(deployment.GetNamespace()),
		StorageNamespace: r.ConfigProvider.GetTargetNamespace(deployment.GetNamespace()),
		DriftDetection:   r.ConfigProvider.GetHelmDriftDetectionMode(deployment.GetNamespace()),
		Install:          r.ConfigProvider.GetHelmInstallConfig(deployment.GetNamespace()),
		CommonMetadata: &helmv2.CommonMetadata{
			Labels: map[string]string{
				"konfidence.cloud/artifact-deployment": deployment.Name,
			},
		},
	}

	return nil
}

func (r *releaseReconciler) getHelmChart(
	ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) *sourcev1.HelmChart {

	// TODO karsten: use helmRelease.Status.HelmChart instead of building name ourselves
	objectKey := types.NamespacedName{
		Namespace: deployment.GetNamespace(),
		Name:      fmt.Sprintf("%s-%s", deployment.GetNamespace(), deployment.Name),
	}
	helmChart := &sourcev1.HelmChart{}

	if err := r.Client.Get(ctx, objectKey, helmChart); err != nil {
		return nil
	}

	return helmChart
}

func (r *releaseReconciler) mapStatusConditionsFromHelmChart(
	deployment *konfidencev1alpha1.ArtifactDeployment, helmChart *sourcev1.HelmChart) bool {

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
		return konfidencev1alpha1.ArtifactFetchedCondition
	default:
		return ""
	}
}

func (r *releaseReconciler) mapStatusConditionsFromHelmRelease(
	deployment *konfidencev1alpha1.ArtifactDeployment, helmRelease *helmv2.HelmRelease) {

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
		return konfidencev1alpha1.ArtifactDeployedCondition
	case "Released":
		return konfidencev1alpha1.AppHealthyCondition
	default:
		return ""
	}
}
