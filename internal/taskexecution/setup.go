package taskexecution

import (
	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/taskexecution/internal/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "TaskExecution"

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	if err := (&internalcontroller.TaskExecutionReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder(internalcontroller.TaskExecutionControllerName),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "TaskExecution")
		return err
	}

	return nil
}
