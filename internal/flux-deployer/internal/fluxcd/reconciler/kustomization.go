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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/flux-deployer/internal/fluxcd/utils"

	// see https://github.com/fluxcd/kustomize-controller/tree/main/api/v1
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/flux-deployer/internal/fluxcd"
)

//
// Flux Kustomization docs: https://fluxcd.io/flux/components/kustomize/kustomizations/
// Flux API reference: https://fluxcd.io/flux/components/kustomize/api/v1/
//

const KustomizationControllerName = "flux-kustomization-controller"

type KustomizationReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
	Recorder       events.EventRecorder
}

var _ fluxcd.FluxKustomizeReconciler = (*KustomizationReconciler)(nil)

func (r *KustomizationReconciler) Reconcile(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, kustomizeResource *fluxcd.KustomizeResource) (isReady bool, err error) {

	kustomization := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      buildResourceName(deployment, &kustomizeResource.OCMResource),
		},
	}
	mutateFn := func() error { return r.mutateKustomization(ctx, deployment, kustomizeResource, kustomization) }

	// create or update the Kustomization resource
	operationResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, kustomization, mutateFn)
	if err != nil {
		return false, fmt.Errorf("failed to reconcile Kustomization: %w", err)
	}
	r.Recorder.Eventf(deployment, nil, corev1.EventTypeNormal, "ReconciledKustomization", "ReconciledKustomization", fmt.Sprintf("Kustomization %s %s", kustomization.Name, operationResult))

	// map the status conditions of the Kustomization to the ArtifactDeployment
	r.mapStatusConditions(deployment, kustomization)

	return meta.IsStatusConditionTrue(kustomization.Status.Conditions, conditionTypeReady), nil
}

func (r *KustomizationReconciler) mutateKustomization(ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, kustomizeResource *fluxcd.KustomizeResource, kustomization *kustomizev1.Kustomization) error {

	// set owner reference (with controller:=true) if newly created
	if kustomization.CreationTimestamp.IsZero() {
		if err := controllerutil.SetControllerReference(deployment, kustomization, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Kustomization: %w", err)
		}
	}

	kubeConfig, err := r.ConfigProvider.GetKubeConfigRef(ctx, deployment.GetNamespace())
	if err != nil {
		return err
	}

	// update spec
	kustomization.Spec = kustomizev1.KustomizationSpec{
		Interval: r.ConfigProvider.GetReconcileInterval(deployment.GetNamespace()),
		SourceRef: kustomizev1.CrossNamespaceSourceReference{
			Kind:      sourcev1.OCIRepositoryKind,
			Namespace: deployment.GetNamespace(),
			Name:      buildResourceName(deployment, &kustomizeResource.OCMResource),
		},
		Path:            kustomizeResource.Path,
		KubeConfig:      kubeConfig,
		TargetNamespace: r.ConfigProvider.GetTargetNamespace(deployment.GetNamespace()),
		NameSuffix:      "-" + deployment.Name[:6],
		Prune:           true,
		Wait:            true,
		CommonMetadata: &kustomizev1.CommonMetadata{
			Labels: map[string]string{
				"konfidence.cloud/artifact-deployment": utils.SanitizeK8sResourceName(deployment.Name),
			},
		},
	}

	return nil
}

func (r *KustomizationReconciler) mapStatusConditions(
	deployment *landscapev1alpha1.ArtifactDeployment, kustomization *kustomizev1.Kustomization) {

	for _, condition := range kustomization.Status.Conditions {
		if conditionType := mapKustomizationConditionType(condition.Type); conditionType != "" {
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

func mapKustomizationConditionType(conditionType string) string {
	switch conditionType {
	case conditionTypeReady:
		return landscapev1alpha1.ArtifactDeployedCondition
	case "Healthy":
		return landscapev1alpha1.AppHealthyCondition
	default:
		return ""
	}
}
