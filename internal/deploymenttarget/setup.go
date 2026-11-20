package deploymenttarget

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	dpltarget "github.com/konfidence-project/konfidence/pkg/deployer/deploymenttarget"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/build"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "DeploymentTarget"

// validate accepts empty parameters for local deployment. Any configured
// parameters must identify a Secret containing a valid kubeconfig.
func validate(ctx context.Context, c client.Reader, dt *konfidencev1alpha1.DeploymentTarget) (dpltarget.ValidationResult, error) {
	secretKey, err := kubeconfigSecretRef(dt)
	if err != nil {
		return dpltarget.ValidationFailure(err.Error()), nil
	}
	if secretKey == nil {
		return dpltarget.ValidationSuccess(), nil
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, *secretKey, secret); err != nil {
		return nil, fmt.Errorf("get secret %s: %w", *secretKey, err)
	}

	kubeconfigBytes, err := kubeconfigFromSecret(secret)
	if err != nil {
		return dpltarget.ValidationFailure(err.Error()), nil
	}

	if _, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes); err != nil {
		return dpltarget.ValidationFailure(fmt.Sprintf("secret %q does not contain a valid kubeconfig", secretKey.Name)), nil
	}
	return dpltarget.ValidationSuccess(), nil
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymenttargets,verbs=get;list;watch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymenttargets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	if err := dpltarget.NewControllerManagedBy(mgr).
		For(internal.SupportedArtifactTypes).
		WithValidation(validate).
		Watches(&corev1.Secret{}, dpltarget.EnqueueReferencedTargets()).
		Named(internal.ControllerName + ":" + build.Version).
		Complete(); err != nil {
		logger.Error(err, "unable to create controller", "controller", "DeploymentTarget")
		return err
	}

	return nil
}
