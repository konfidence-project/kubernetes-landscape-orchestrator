package deploymenttarget

import (
	"errors"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/kubernetesclient"
	"k8s.io/apimachinery/pkg/types"
)

// Resolve validates and interprets a target without accessing Kubernetes.
// Target selection and readiness checks remain the caller's responsibility.
func Resolve(target *konfidencev1alpha1.DeploymentTarget) (kubernetesclient.Destination, error) {
	if target == nil {
		return kubernetesclient.Destination{}, errors.New("deployment target is required")
	}

	params := target.Spec.Parameters
	if params == nil {
		return kubernetesclient.Destination{Local: true}, nil
	}
	if err := validateRemoteParameters(params); err != nil {
		return kubernetesclient.Destination{}, err
	}

	return kubernetesclient.Destination{
		SecretRef: types.NamespacedName{Namespace: target.Namespace, Name: params.Name},
	}, nil
}

func validateRemoteParameters(params *konfidencev1alpha1.Parameters) error {
	if params.APIGroup != "" || params.Kind != "Secret" || params.Name == "" {
		return errors.New("parameters must refer to a Kubernetes Secret containing a kubeconfig")
	}
	return nil
}
