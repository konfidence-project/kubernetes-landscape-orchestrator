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

package controller

import (
	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/flux-deployer/internal/controller"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/flux-deployer/internal/fluxcd/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "flux-deployer"

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	configProvider := &internalcontroller.ConfigProvider{Client: mgr.GetClient()}

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
