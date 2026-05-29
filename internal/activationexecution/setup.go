package activationexecution

import (
	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/activationexecution/internal/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "ActivationExecution"

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	if err := (&internalcontroller.ActivationTaskExecutionReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder(internalcontroller.ActivationTaskExecutionControllerName),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "ActivationTaskExecution")
		return err
	}

	return nil
}
