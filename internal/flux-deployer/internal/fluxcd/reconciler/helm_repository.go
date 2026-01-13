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
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// see https://github.com/konfidence-project/crds/tree/main/api/landscape/v1alpha1
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd"
)

//
// Flux HelmRepository docs: https://fluxcd.io/flux/components/source/helmrepositories/
// Flux API reference: https://fluxcd.io/flux/components/source/api/v1/#source.toolkit.fluxcd.io/v1.HelmRepository
//

type HelmRepositoryReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
}

var _ fluxcd.FluxHelmReconciler = (*HelmRepositoryReconciler)(nil)

func (r *HelmRepositoryReconciler) Reconcile(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, helmChartResource *fluxcd.HelmChartResource) (isReady bool, err error) {

	helmRepository := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      buildHelmRepositoryResourceName(deployment, &helmChartResource.OCMResource),
		},
	}

	mutateFn := func() error { return r.mutateHelmRepository(ctx, deployment, helmChartResource, helmRepository) }

	// create or update the HelmRepository resource
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, helmRepository, mutateFn); err != nil {
		return false, fmt.Errorf("failed to reconcile HelmRepository: %w", err)
	}

	// HelmRepository itself has no status conditions; cannot map it to ArtifactDeployment status conditions

	return true, nil
}

func (r *HelmRepositoryReconciler) mutateHelmRepository(
	ctx context.Context, deployment *landscapev1alpha1.ArtifactDeployment, helmChartResource *fluxcd.HelmChartResource, helmRepository *sourcev1.HelmRepository) error {

	// set owner reference (with controller:=true) if newly created
	if helmRepository.CreationTimestamp.IsZero() {
		if err := controllerutil.SetControllerReference(deployment, helmRepository, r.Scheme); err != nil {
			return fmt.Errorf("failed to set owner reference on HelmRepository: %w", err)
		}
	}

	secretRef, err := getSecretRef(ctx, r.Client, deployment, helmChartResource.Repository)
	if err != nil {
		return fmt.Errorf("failed to resolve secretRef for Helm Repository: %w", err)
	}

	// update spec
	helmRepository.Spec = sourcev1.HelmRepositorySpec{
		Interval:  r.ConfigProvider.GetReconcileInterval(deployment.GetNamespace()),
		URL:       helmChartResource.Repository,
		Insecure:  isInsecure(deployment),
		SecretRef: secretRef,
	}

	if strings.HasPrefix(helmRepository.Spec.URL, "oci://") {
		helmRepository.Spec.Type = sourcev1.HelmRepositoryTypeOCI
	}

	return nil
}
