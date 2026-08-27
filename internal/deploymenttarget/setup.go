package deploymenttarget

import (
	"github.com/go-logr/logr"
	internalcontroller "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/deploymenttarget/internal/controller"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "DeploymentTarget"

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	if err := (&internalcontroller.DeploymentTargetReconciler{
		Client: mgr.GetClient(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to create controller", "controller", "DeploymentTarget")
		return err
	}

	return nil
}
