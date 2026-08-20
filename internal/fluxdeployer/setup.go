package fluxdeployer

import (
	"context"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/controller"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deployer"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	OperatorFlagName = "FluxDeployer"
	ControllerName   = "konfidence.cloud/kubernetes-landscape-orchestrator"
)

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	if err := internalcontroller.IndexReadyDeploymentTargets(context.Background(), mgr.GetFieldIndexer()); err != nil {
		return err
	}

	configProvider := &internalcontroller.ConfigProvider{
		Client: mgr.GetClient(),
	}

	helmDeployer := deployer.NewArtifactDeploymentReconciler(mgr.GetClient(), ControllerName, internalcontroller.ManifestTypeHelm).
		Owns(&sourcev1.HelmRepository{}).
		Owns(&sourcev1.HelmChart{}).
		Owns(&helmv2.HelmRelease{}).
		Complete(&internalcontroller.HelmArtifactDeploymentReconciler{Client: mgr.GetClient()})
	if err := helmDeployer.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create helm controller")
		return err
	}

	kustomizeDeployer := deployer.NewArtifactDeploymentReconciler(mgr.GetClient(), ControllerName, internalcontroller.ManifestTypeKustomize).
		Owns(&sourcev1.OCIRepository{}).
		Owns(&kustomizev1.Kustomization{}).
		Complete(&internalcontroller.KustomizeArtifactDeploymentReconciler{Client: mgr.GetClient()})
	if err := kustomizeDeployer.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create kustomize controller")
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
