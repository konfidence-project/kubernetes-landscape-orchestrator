//nolint:staticcheck // ST1001: allow dot-import for test utils using Gomega
package utils

import (
	"context"

	landscape "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateActivationExecution(ctx context.Context, k8sClient client.Client, name string, namespace string, specName string, specType string, executionSpec string) {
	activationExecution := &landscape.ActivationExecution{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "landscape.konfidence.cloud/v1alpha1",
			Kind:       "ActivationExecution",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: landscape.ActivationExecutionSpec{
			Name: specName,
			Type: specType,
			Spec: runtime.RawExtension{
				Raw: []byte(executionSpec),
			},
		},
	}

	Expect(k8sClient.Create(ctx, activationExecution)).To(Succeed())
}

func GetActivationExecution(ctx context.Context, k8sClient client.Client, name string, namespace string, opt bool) *landscape.ActivationExecution {
	activationExecution := &landscape.ActivationExecution{}
	activationExecutionLookupKey := types.NamespacedName{Name: name, Namespace: namespace}
	err := k8sClient.Get(ctx, activationExecutionLookupKey, activationExecution)

	if opt && err != nil && errors.IsNotFound(err) {
		return nil
	}

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch activationExecution: %s", name)
	return activationExecution
}

func DeleteActivationExecution(ctx context.Context, k8sClient client.Client, activationExecution *landscape.ActivationExecution) {
	err := k8sClient.Delete(ctx, activationExecution)
	Expect(err).ToNot(HaveOccurred(), "Failed to delete activationExecution: %s", activationExecution.Name)
}

func CleanupActivationExecution(k8sClient client.Client, activationExecutionName string, namespace string) {
	ctx := context.Background()
	activationExecution := GetActivationExecution(ctx, k8sClient, activationExecutionName, namespace, true)

	if activationExecution != nil {
		DeleteActivationExecution(ctx, k8sClient, activationExecution)
	}
}
