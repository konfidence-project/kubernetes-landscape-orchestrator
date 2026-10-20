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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

func usesSecret(dt *konfidencev1alpha1.DeploymentTarget, secret *corev1.Secret) bool {
	return dt.Spec.Parameters != nil && dt.Spec.Parameters.APIGroup == "" && dt.Spec.Parameters.Kind == "Secret" && dt.Spec.Parameters.Name == secret.Name
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymenttargets,verbs=get;list;watch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymenttargets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func SetupControllers(mgr manager.Manager, logger logr.Logger) error {
	c := mgr.GetClient()

	secretMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			secret, ok := obj.(*corev1.Secret)
			if !ok {
				return nil
			}

			// TODO karsten: can we optimize this and fetch a filtered list?
			targets := &konfidencev1alpha1.DeploymentTargetList{}
			if err := c.List(ctx, targets, client.InNamespace(secret.Namespace)); err != nil {
				return nil
			}
			requests := make([]reconcile.Request, 0)
			for _, dt := range targets.Items {
				if internal.SupportedArtifactTypes.Contains(dt.ArtifactType()) && usesSecret(&dt, secret) {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&dt)})
				}
			}
			return requests
		},
	)

	if err := dpltarget.NewControllerManagedBy(mgr).
		For(internal.SupportedArtifactTypes).
		WithValidation(validate).
		Watches(&corev1.Secret{}, secretMapper).
		Named(internal.ControllerName + ":" + build.Version).
		Complete(); err != nil {
		logger.Error(err, "unable to create controller", "controller", "DeploymentTarget")
		return err
	}

	return nil
}
