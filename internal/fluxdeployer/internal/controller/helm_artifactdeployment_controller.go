package controller

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	// see https://github.com/konfidence-project/konfidence/tree/main/api/v1alpha1
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deployer"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
)

// HelmArtifactDeploymentReconciler reconciles ArtifactDeployment objects where manifest type is 'Helm'
type HelmArtifactDeploymentReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=artifactdeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmcharts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *HelmArtifactDeploymentReconciler) Reconcile(ctx context.Context, deployment *konfidencev1alpha1.ArtifactDeployment) (deployer.ReconcileResult, error) {
	// TODO karsten: remove ReconcileOld and implement logic here
	panic("implement me")
}

func (r *HelmArtifactDeploymentReconciler) ReconcileOld(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("start reconciling Helm artifact deployment")

	// get the ArtifactDeployment object
	deployment := &konfidencev1alpha1.ArtifactDeployment{}
	if err := r.Get(ctx, req.NamespacedName, deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to get artifact deployment object: %w", err)
	}

	// Only reconcile ArtifactDeployments whose manifest type is currently covered by
	// an active DeploymentClass we own. This check uses the informer cache.
	activeTypes, err := deploymentclass.ActiveTypes(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve active deployment class types: %w", err)
	}
	if _, active := activeTypes[deployment.Spec.Manifest.Type]; !active {
		return ctrl.Result{}, nil
	}

	// preserve original deployment status for patching it later
	originalDeployment := deployment.DeepCopy()
	patch := client.MergeFrom(originalDeployment)

	if err := r.ArtifactDeployer.Reconcile(ctx, deployment); err != nil {
		targetNotReady := setDeploymentTargetNotReady(deployment, err)
		if !reflect.DeepEqual(deployment.Status, originalDeployment.Status) {
			if patchErr := r.Client.Status().Patch(ctx, deployment, patch); patchErr != nil {
				return ctrl.Result{}, fmt.Errorf("patch ArtifactDeployment status: %w", patchErr)
			}
		}
		if targetNotReady {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	err = r.DeploymentResultStatusUpdater.MutateStatus(ctx, deployment)
	if err != nil {
		log.Error(err, "failed to handle Helm artifact deployment result", "ArtifactDeployment", deployment)
	}

	err = r.ReadyConditionStatusUpdater.MutateStatus(ctx, deployment)
	if err != nil {
		log.Error(err, "failed to mutate status condition to READY ", "ArtifactDeployment", deployment)
	}

	// patch the deployment status updates
	if !reflect.DeepEqual(deployment.Status, originalDeployment.Status) {
		if err := r.Client.Status().Patch(ctx, deployment, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to patch artifact deployment status: %w", err)
		}
	}

	log.Info("finish reconciling Helm artifact deployment")
	return ctrl.Result{}, nil
}
