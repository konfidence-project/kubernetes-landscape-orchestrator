package reconciler

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	// see https://github.com/fluxcd/source-controller/tree/main/api/v1
	sourcev1 "github.com/fluxcd/source-controller/api/v1"

	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
)

// Flux HelmRepository docs: https://fluxcd.io/flux/components/source/helmrepositories/
// Flux API reference: https://fluxcd.io/flux/components/source/api/v1/#source.toolkit.fluxcd.io/v1.HelmRepository

const HelmRepositoryControllerName = "flux-helm-repository-controller"

type HelmRepositoryReconciler struct {
	Client         client.Client
	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
	Recorder       events.EventRecorder
}

var _ fluxcd.FluxHelmReconciler = (*HelmRepositoryReconciler)(nil)

func (r *HelmRepositoryReconciler) Reconcile(
	ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment, helmChartResource *fluxcd.HelmChartResource) (isReady bool, err error) {

	helmRepository := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deployment.GetNamespace(),
			Name:      deployment.Name,
		},
	}

	mutateFn := func() error { return r.mutateHelmRepository(ctx, deployment, helmChartResource, helmRepository) }

	// create or update the HelmRepository resource
	operationResult, err := controllerutil.CreateOrUpdate(ctx, r.Client, helmRepository, mutateFn)
	if err != nil {
		return false, fmt.Errorf("failed to reconcile HelmRepository: %w", err)
	}
	r.Recorder.Eventf(deployment, nil, corev1.EventTypeNormal,
		"HelmRepositoryReconciled", "HelmRepositoryReconciled",
		fmt.Sprintf("HelmRepository %s %s", helmRepository.Name, operationResult))
	// HelmRepository itself has no status conditions; cannot map it to ArtifactDeployment status conditions

	return true, nil
}

func (r *HelmRepositoryReconciler) mutateHelmRepository(
	ctx context.Context,
	deployment *konfidencev1alpha1.ArtifactDeployment,
	helmChartResource *fluxcd.HelmChartResource,
	helmRepository *sourcev1.HelmRepository,
) error {

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
