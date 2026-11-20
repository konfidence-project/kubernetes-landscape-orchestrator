package activationexecution

import (
	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/activationexecution/internal/controller"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/deploymenttarget"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "ActivationExecution"

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	clientProvider, err := deploymenttarget.NewKubernetesClientProvider(mgr.GetClient(), mgr.GetAPIReader())
	if err != nil {
		logger.Error(err, "unable to create deployment target client provider", "controller", "ActivationTaskExecution")
		return err
	}
	if err := (&internalcontroller.ActivationTaskExecutionReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Recorder:       mgr.GetEventRecorder(internalcontroller.ActivationTaskExecutionControllerName),
		ClientProvider: clientProvider,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "ActivationTaskExecution")
		return err
	}

	if err := (&internalcontroller.StageRouteCleanupReconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		ClientProvider: clientProvider,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "StageRouteCleanup")
		return err
	}

	return nil
}
