package fluxdeployer

import (
	"context"

	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/controller"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "FluxDeployer"

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	if err := internalcontroller.IndexReadyDeploymentTargets(context.Background(), mgr.GetFieldIndexer()); err != nil {
		return err
	}

	configProvider := &internalcontroller.ConfigProvider{
		Client: mgr.GetClient(),
	}

	if err := (&internalcontroller.HelmArtifactDeploymentReconciler{
		Client:                        mgr.GetClient(),
		ReadyConditionStatusUpdater:   &internalcontroller.ReadyConditionStatusUpdater{},
		DeploymentResultStatusUpdater: &internalcontroller.DeploymentResultStatusUpdater{Client: mgr.GetClient()},
		HelmRepositoryReconciler: &reconciler.HelmRepositoryReconciler{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			ConfigProvider: configProvider,
			Recorder:       mgr.GetEventRecorder(reconciler.HelmRepositoryControllerName),
		},
		HelmReleaseReconciler: &reconciler.HelmReleaseReconciler{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			ConfigProvider: configProvider,
		},
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create helm controller", "controller", "ArtifactDeployment")
		return err
	}

	if err := (&internalcontroller.KustomizeArtifactDeploymentReconciler{
		Client:                        mgr.GetClient(),
		ReadyConditionStatusUpdater:   &internalcontroller.ReadyConditionStatusUpdater{},
		DeploymentResultStatusUpdater: &internalcontroller.DeploymentResultStatusUpdater{Client: mgr.GetClient()},
		OCIRepositoryReconciler: &reconciler.OCIRepositoryReconciler{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			ConfigProvider: configProvider,
			Recorder:       mgr.GetEventRecorder(reconciler.OCIRepositoryControllerName),
		},
		KustomizationReconciler: &reconciler.KustomizationReconciler{
			Client:         mgr.GetClient(),
			Scheme:         mgr.GetScheme(),
			ConfigProvider: configProvider,
			Recorder:       mgr.GetEventRecorder(reconciler.KustomizationControllerName),
		},
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create kustomize controller", "controller", "ArtifactDeployment")
		return err
	}

	if err := (&internalcontroller.VectorAssignmentReconciler{
		Client:   mgr.GetClient(),
		Recorder: mgr.GetEventRecorder(internalcontroller.VectorAssignmentControllerName),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create vector assignment controller", "controller", "VectorAssignment")
		return err
	}

	return nil
}
