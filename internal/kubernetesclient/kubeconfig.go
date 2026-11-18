package kubernetesclient

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// KubeconfigFromSecret returns the first present kubeconfig key, even if its value is empty
// or invalid. The key priority follows Flux's value, then value.yaml convention.
func KubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	for _, key := range []string{"value", "value.yaml"} {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("secret %q contains no kubeconfig: expected key as one of [value value.yaml]", secret.Name)
}
