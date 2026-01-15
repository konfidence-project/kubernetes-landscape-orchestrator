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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd"
)

//
// Flux OCIRepository docs: https://fluxcd.io/flux/components/source/ocirepositories/
// Flux API reference: https://fluxcd.io/flux/components/source/api/v1/#source.toolkit.fluxcd.io/v1.OCIRepository
//

const OCIRepositoryControllerName = "flux-oci-repository-reconciler"

type OCIRepositoryReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
	Recorder       record.EventRecorder
}

var _ fluxcd.FluxKustomizeReconciler = (*OCIRepositoryReconciler)(nil)

func (r *OCIRepositoryReconciler) Reconcile(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, kustomizeResource *fluxcd.KustomizeResource) (isReady bool, err error) {

	ociRepository := &sourcev1.OCIRepository{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      buildResourceName(deployment, &kustomizeResource.OCMResource),
		},
	}
	mutateFn := func() error { return r.mutateOCIRepository(ctx, deployment, kustomizeResource, ociRepository) }

	// create or update the OCIRepository resource
	operationResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, ociRepository, mutateFn)
	if err != nil {
		return false, fmt.Errorf("failed to reconcile OCIRepository: %w", err)
	}
	r.Recorder.Event(deployment, corev1.EventTypeNormal, "OCIRepositoryReconciled", fmt.Sprintf("OCIRepository %s %s", ociRepository.Name, operationResult))

	// map the status conditions of the OCIRepository to the ArtifactDeployment
	r.mapStatusConditions(deployment, ociRepository)

	return meta.IsStatusConditionTrue(ociRepository.Status.Conditions, "Ready"), nil
}

func (r *OCIRepositoryReconciler) mutateOCIRepository(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, kustomizeResource *fluxcd.KustomizeResource, ociRepository *sourcev1.OCIRepository) error {

	// set owner reference (with controller:=true) if newly created
	if ociRepository.CreationTimestamp.IsZero() {
		if err := controllerutil.SetControllerReference(deployment, ociRepository, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on OCIRepository: %w", err)
		}
	}

	secretRef, err := getSecretRef(ctx, r.Client, deployment, kustomizeResource.Repository)
	if err != nil {
		return fmt.Errorf("failed to resolve secretRef for OCI Repository: %w", err)
	}

	// update spec
	ociRepository.Spec = sourcev1.OCIRepositorySpec{
		Interval:  r.ConfigProvider.GetReconcileInterval(deployment.GetNamespace()),
		URL:       kustomizeResource.Repository,
		Insecure:  isInsecure(deployment),
		SecretRef: secretRef,
		Reference: &sourcev1.OCIRepositoryRef{
			Tag: kustomizeResource.Tag,
		},
	}

	return nil
}

func (r *OCIRepositoryReconciler) mapStatusConditions(
	deployment *landscapev1alpha1.ArtifactDeployment, ociRepository *sourcev1.OCIRepository) {

	for _, condition := range ociRepository.Status.Conditions {
		if conditionType := mapOCIRepositoryConditionType(condition.Type); conditionType != "" {
			meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
				Type:    conditionType,
				Status:  condition.Status,
				Reason:  condition.Reason,
				Message: condition.Message,
			})
		}
	}
}

func mapOCIRepositoryConditionType(conditionType string) string {
	switch conditionType {
	case conditionTypeReady:
		return landscapev1alpha1.ArtifactFetchedCondition
	default:
		return ""
	}
}
