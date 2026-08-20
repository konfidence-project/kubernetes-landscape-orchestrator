package helm

import (
	"context"
	"fmt"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deployer"
)

// Reconciler reconciles ArtifactDeployment objects where manifest type is 'Helm'
type Reconciler struct {
	client.Client

	Scheme         *runtime.Scheme
	ConfigProvider fluxcd.FluxConfigProvider
	Recorder       events.EventRecorder

	DeploymentResulter deployer.DeploymentResulter
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *Reconciler) Reconcile(ctx context.Context, ad *konfidencev1alpha1.ArtifactDeployment) (deployer.ReconcileResult, error) {
	// TODO karsten: optimize how deployment targets are resolved here, errors should basically not happen
	kubeConfig, err := r.ConfigProvider.GetKubeConfigRef(ctx, ad.Namespace, ad.Spec.Manifest.Type)
	if err != nil {
		return deployer.ReconcileResult{}, fmt.Errorf("failed to get kubeconfig for ArtifactDeployment %s/%s: %w", ad.Namespace, ad.Name, err)
	}

	resource, err := utils.SingleOCMResource(ad.Spec.Component, ocmResourceTypeHelmChart)
	if err != nil {
		condition := metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ArtifactDeploymentReasonInvalidConstraint,
			Message:            err.Error(),
			ObservedGeneration: ad.Generation,
			LastTransitionTime: metav1.Now(),
		}
		return deployer.ReconcileResult{Conditions: []metav1.Condition{condition}}, nil
	}

	helmResource, err := fluxcd.Map(*resource).ToHelm()
	if err != nil {
		condition := metav1.Condition{
			Type:               konfidencev1alpha1.ArtifactDeploymentReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             ArtifactDeploymentReasonInvalidConstraint,
			Message:            err.Error(),
			ObservedGeneration: ad.Generation,
			LastTransitionTime: metav1.Now(),
		}
		return deployer.ReconcileResult{Conditions: []metav1.Condition{condition}}, nil
	}

	helmRepository := &repositoryReconciler{
		Client:         r.Client,
		Scheme:         r.Scheme,
		ConfigProvider: r.ConfigProvider,
		Recorder:       r.Recorder,
	}
	if _, err := helmRepository.Reconcile(ctx, ad, helmResource); err != nil {
		return deployer.ReconcileResult{}, fmt.Errorf("reconcile HelmRepository: %w", err)
	}

	helmRelease := &releaseReconciler{
		Client:         r.Client,
		Scheme:         r.Scheme,
		ConfigProvider: r.ConfigProvider,
	}
	if _, err := helmRelease.Reconcile(ctx, ad, helmResource, kubeConfig); err != nil {
		return deployer.ReconcileResult{}, fmt.Errorf("reconcile HelmRelease: %w", err)
	}

	results, err := r.DeploymentResulter.GetDeploymentResults(ctx, ad)
	if err != nil {
		return deployer.ReconcileResult{}, fmt.Errorf("failed to get deployment results: %w", err)
	}

	var conditions []metav1.Condition
	conditions = append(conditions, metav1.Condition{
		Type:               konfidencev1alpha1.DeploymentResultCreatedCondition,
		Status:             metav1.ConditionTrue,
		Reason:             konfidencev1alpha1.DeploymentResultCreatedCondition,
		Message:            "Successfully created DeploymentResult",
		ObservedGeneration: ad.Generation,
		LastTransitionTime: metav1.Now(),
	})

	return deployer.ReconcileResult{
		Conditions:        conditions,
		DeploymentResults: results,
	}, nil
}

// TODO karsten: move to proper place
const ArtifactDeploymentReasonInvalidConstraint = "ArtifactDeploymentInvalidConstraint"
