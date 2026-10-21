package deploymenttarget

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	dpltarget "github.com/konfidence-project/konfidence/pkg/deployer/deployment-target"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/build"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const OperatorFlagName = "DeploymentTarget"

// Currently the user cannot specify which key should be used in a Secret/ConfigMap, so we
// have to fall back to the default keys used by FluxCD:
// https://fluxcd.io/flux/components/kustomize/kustomizations/#secret-based-authentication
var acceptedKubeconfigKeys = []string{"value", "value.yaml"}

// validate accepts empty parameters for local deployment. Any configured
// parameters must identify a Secret containing a valid kubeconfig.
func validate(ctx context.Context, c client.Reader, dt *konfidencev1alpha1.DeploymentTarget) error {
	params := dt.Spec.Parameters
	if params == nil {
		// empty parameters is allowed and deploys to local cluster
		return nil
	}

	if params.APIGroup != "" || params.Kind != "Secret" || params.Name == "" {
		return fmt.Errorf("parameters must refer to a Kubernetes Secret containing a kubeconfig")
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Namespace: dt.Namespace, Name: params.Name}
	if err := c.Get(ctx, secretKey, secret); err != nil {
		return fmt.Errorf("get secret %s: %w", secretKey, err)
	}

	kubeconfigBytes, err := kubeconfigFromSecret(secret)
	if err != nil {
		return err
	}

	if _, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes); err != nil {
		return fmt.Errorf("secret %q does not contain a valid kubeconfig: %w", params.Name, err)
	}
	return nil
}

func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	for _, key := range acceptedKubeconfigKeys {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("secret %q contains no kubeconfig: expected key as one of [value value.yaml]", secret.Name)
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
