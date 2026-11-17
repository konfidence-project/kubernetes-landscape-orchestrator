package taskexecution

import (
	"github.com/go-logr/logr"
	pkgctrl "github.com/konfidence-project/konfidence/pkg/controller"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/taskexecution/internal/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "TaskExecution"

func SetupControllers(mgr manager.Manager, logger logr.Logger, excludedNamespaces ...pkgctrl.NamespaceExclusions) error {
	if err := (&internalcontroller.TaskExecutionReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorder(internalcontroller.TaskExecutionControllerName),
	}).SetupWithManager(mgr, excludedNamespaces...); err != nil {
		logger.Error(err, "unable to create controller", "controller", "TaskExecution")
		return err
	}

	return nil
}
