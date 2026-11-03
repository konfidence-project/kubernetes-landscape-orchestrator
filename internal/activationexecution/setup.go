package activationexecution

import (
	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/activationexecution/internal/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "ActivationExecution"

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	landscape := &internalcontroller.LandscapeClientProvider{
		Local:  mgr.GetClient(),
		Reader: mgr.GetAPIReader(),
		Scheme: mgr.GetScheme(),
	}

	if err := (&internalcontroller.ActivationTaskExecutionReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Recorder:  mgr.GetEventRecorder(internalcontroller.ActivationTaskExecutionControllerName),
		Landscape: landscape,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "ActivationTaskExecution")
		return err
	}

	if err := (&internalcontroller.StageRouteCleanupReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Landscape: landscape,
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "StageRouteCleanup")
		return err
	}

	return nil
}
