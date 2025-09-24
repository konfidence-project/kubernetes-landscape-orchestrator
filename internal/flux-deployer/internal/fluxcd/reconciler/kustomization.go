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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// see https://github.com/fluxcd/kustomize-controller/tree/main/api/v1
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd/utils"
)

//
// Flux Kustomization docs: https://fluxcd.io/flux/components/kustomize/kustomizations/
// Flux API reference: https://fluxcd.io/flux/components/kustomize/api/v1/
//

type KustomizationReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
}

var _ fluxcd.FluxReconciler = (*KustomizationReconciler)(nil)

func (r *KustomizationReconciler) Reconcile(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) (isReady bool, err error) {

	kustomization := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      buildResourceName(deployment, ocmResource),
		},
	}
	mutateFn := func() error { return r.mutateKustomization(deployment, ocmResource, kustomization) }

	// create or update the Kustomization resource
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, kustomization, mutateFn); err != nil {
		return false, fmt.Errorf("failed to reconcile Kustomization: %w", err)
	}

	// map the status conditions of the Kustomization to the ArtifactDeployment
	r.mapStatusConditions(deployment, kustomization)

	return meta.IsStatusConditionTrue(kustomization.Status.Conditions, "Ready"), nil
}

func (r *KustomizationReconciler) mutateKustomization(
	deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource, kustomization *kustomizev1.Kustomization) error {

	// set owner reference (with controller:=true) if newly created
	if kustomization.ObjectMeta.CreationTimestamp.IsZero() {
		if err := controllerutil.SetControllerReference(deployment, kustomization, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on Kustomization: %w", err)
		}
	}

	// update spec
	kustomization.Spec = kustomizev1.KustomizationSpec{
		Interval: r.ConfigProvider.GetReconcileInterval(deployment.GetNamespace()),
		SourceRef: kustomizev1.CrossNamespaceSourceReference{
			Kind:      sourcev1.OCIRepositoryKind,
			Namespace: deployment.GetNamespace(),
			Name:      buildResourceName(deployment, ocmResource),
		},
		Path:            "./",
		KubeConfig:      r.ConfigProvider.GetKubeConfigRef(deployment.GetNamespace()),
		TargetNamespace: r.ConfigProvider.GetTargetNamespace(deployment.GetNamespace()),
		NameSuffix:      fmt.Sprintf("-%s", utils.Must(utils.GetKonfidenceLabel(&deployment.ObjectMeta, "vector-deployment-id"))),
		Prune:           true,
		Wait:            true,
	}

	return nil
}

func (r *KustomizationReconciler) mapStatusConditions(
	deployment *landscapev1alpha1.ArtifactDeployment, kustomization *kustomizev1.Kustomization) {

	for _, condition := range kustomization.Status.Conditions {
		if conditionType := mapKustomizationConditionType(condition.Type); conditionType != "" {
			meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
				Type:    conditionType,
				Status:  condition.Status,
				Reason:  condition.Reason,
				Message: condition.Message,
			})
		}
	}
}

func mapKustomizationConditionType(conditionType string) string {
	switch conditionType {
	case "Ready":
		return landscapev1alpha1.ArtifactDeployedCondition
	case "Healthy":
		return landscapev1alpha1.AppHealthyCondition
	default:
		return ""
	}
}
