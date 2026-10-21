//nolint:staticcheck // ST1001: allow dot-import for test utils using Gomega
package utils

import (
	"context"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

func CreateActivationTaskExecution(
	ctx context.Context, k8sClient client.Client,
	name string, namespace string, specType string, executionSpec string, vectorActivation string,
) {
	activationTaskExecution := &konfidencev1alpha1.ActivationTaskExecution{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "konfidence.cloud/v1alpha1",
			Kind:       konfidencev1alpha1.ActivationTaskExecutionKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konfidencev1alpha1.ActivationTaskExecutionSpec{
			Type: specType,
			Spec: runtime.RawExtension{
				Raw: []byte(executionSpec),
			},
			VectorActivation: vectorActivation,
		},
	}

	Expect(k8sClient.Create(ctx, activationTaskExecution)).To(Succeed())
}

func GetActivationTaskExecution(
	ctx context.Context,
	k8sClient client.Client,
	name string,
	namespace string,
	opt bool,
) *konfidencev1alpha1.ActivationTaskExecution {
	activationTaskExecution := &konfidencev1alpha1.ActivationTaskExecution{}
	activationTaskExecutionLookupKey := types.NamespacedName{Name: name, Namespace: namespace}
	err := k8sClient.Get(ctx, activationTaskExecutionLookupKey, activationTaskExecution)

	if opt && err != nil && errors.IsNotFound(err) {
		return nil
	}

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch activationTaskExecution: %s", name)
	return activationTaskExecution
}

func DeleteActivationTaskExecution(ctx context.Context, k8sClient client.Client, activationTaskExecution *konfidencev1alpha1.ActivationTaskExecution) {
	err := k8sClient.Delete(ctx, activationTaskExecution)
	Expect(err).ToNot(HaveOccurred(), "Failed to delete activationTaskExecution: %s", activationTaskExecution.Name)
}

func CleanupActivationTaskExecution(k8sClient client.Client, activationTaskExecutionName string, namespace string) {
	ctx := context.Background()
	activationTaskExecution := GetActivationTaskExecution(ctx, k8sClient, activationTaskExecutionName, namespace, true)

	if activationTaskExecution != nil {
		DeleteActivationTaskExecution(ctx, k8sClient, activationTaskExecution)
	}
}

func DeleteHttpRoute(ctx context.Context, k8sClient client.Client, httpRoute *gwapiv1.HTTPRoute) {
	err := k8sClient.Delete(ctx, httpRoute)
	Expect(err).ToNot(HaveOccurred(), "Failed to delete httpRoute: %s", httpRoute.Name)
}

func GetHttpRoutes(ctx context.Context, k8sClient client.Client) *gwapiv1.HTTPRouteList {
	httpRoutes := &gwapiv1.HTTPRouteList{}
	err := k8sClient.List(ctx, httpRoutes)

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch httpRoutes")
	return httpRoutes
}

func CleanupHttpRoutes(k8sClient client.Client) {
	ctx := context.Background()
	httpRoutes := GetHttpRoutes(ctx, k8sClient)

	for _, httpRoute := range httpRoutes.Items {
		DeleteHttpRoute(ctx, k8sClient, &httpRoute)
	}
}

func GetGateways(ctx context.Context, k8sClient client.Client) *gwapiv1.GatewayList {
	gateways := &gwapiv1.GatewayList{}
	err := k8sClient.List(ctx, gateways)

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch gateways")
	return gateways
}

func CleanupGateways(k8sClient client.Client) {
	ctx := context.Background()
	gateways := GetGateways(ctx, k8sClient)

	for _, gateway := range gateways.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &gateway))).To(Succeed(), "Failed to delete gateway: %s", gateway.Name)
	}
}

// SetGatewayAddresses stands in for the Gateway implementation, which envtest does not run.
func SetGatewayAddresses(ctx context.Context, k8sClient client.Client, gateway *gwapiv1.Gateway, addresses ...gwapiv1.GatewayStatusAddress) {
	gateway.Status.Addresses = addresses
	Expect(k8sClient.Status().Update(ctx, gateway)).To(Succeed())
}

func CreateVectorActivation(
	ctx context.Context, k8sClient client.Client,
	name string, namespace string, stageName string, stageVersionName, vectorName string, vectorDeploymentName string,
) {
	vectorActivation := &konfidencev1alpha1.VectorActivation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "common.konfidence.cloud/v1alpha1",
			Kind:       konfidencev1alpha1.VectorActivationKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konfidencev1alpha1.VectorActivationSpec{
			Stage:            stageName,
			StageVersion:     stageVersionName,
			Vector:           vectorName,
			VectorDeployment: vectorDeploymentName,
		},
	}

	Expect(k8sClient.Create(ctx, vectorActivation)).To(Succeed())
}

func DeleteVectorActivation(ctx context.Context, k8sClient client.Client, vectorActivation *konfidencev1alpha1.VectorActivation) {
	err := k8sClient.Delete(ctx, vectorActivation)
	Expect(err).ToNot(HaveOccurred(), "Failed to delete vectorActivation: %s", vectorActivation.Name)
}

func GetVectorActivations(ctx context.Context, k8sClient client.Client) *konfidencev1alpha1.VectorActivationList {
	vectorActivations := &konfidencev1alpha1.VectorActivationList{}
	err := k8sClient.List(ctx, vectorActivations)

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch vectorActivations")
	return vectorActivations
}

func CleanupVectorActivations(k8sClient client.Client) {
	ctx := context.Background()
	vectorActivations := GetVectorActivations(ctx, k8sClient)

	for _, vectorActivation := range vectorActivations.Items {
		DeleteVectorActivation(ctx, k8sClient, &vectorActivation)
	}
}

func CreateVectorDeployment(ctx context.Context, k8sClient client.Client, name string, namespace string, vector string) {
	vectorDeployment := &konfidencev1alpha1.VectorDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: konfidencev1alpha1.VectorDeploymentSpec{
			Vector: vector,
		},
	}

	Expect(k8sClient.Create(ctx, vectorDeployment)).To(Succeed())
}

func DeleteVectorDeployment(ctx context.Context, k8sClient client.Client, vectorDeployment *konfidencev1alpha1.VectorDeployment) {
	err := k8sClient.Delete(ctx, vectorDeployment)
	Expect(err).ToNot(HaveOccurred(), "Failed to delete vectorDeployment: %s", vectorDeployment.Name)
}

func GetVectorDeployments(ctx context.Context, k8sClient client.Client) *konfidencev1alpha1.VectorDeploymentList {
	vectorDeployments := &konfidencev1alpha1.VectorDeploymentList{}
	err := k8sClient.List(ctx, vectorDeployments)

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch vectorDeployments")
	return vectorDeployments
}

func CleanupVectorDeployments(k8sClient client.Client) {
	ctx := context.Background()
	vectorDeployments := GetVectorDeployments(ctx, k8sClient)

	for _, vectorDeployment := range vectorDeployments.Items {
		DeleteVectorDeployment(ctx, k8sClient, &vectorDeployment)
	}
}

func UpdateVectorDeploymentStatus(ctx context.Context, k8sClient client.Client, vectorDeployment *konfidencev1alpha1.VectorDeployment) {
	err := k8sClient.Status().Update(ctx, vectorDeployment)
	Expect(err).ToNot(HaveOccurred(), "Failed to update status of vectorDeployment: %s", vectorDeployment.Name)
}

func HasOwnerReference(ownerReferences []metav1.OwnerReference, ref metav1.OwnerReference) bool {
	for _, ownerReference := range ownerReferences {
		if ownerReference.Kind == ref.Kind && ownerReference.Name == ref.Name {
			return true
		}
	}

	return false
}

func CreateStage(ctx context.Context, k8sClient client.Client, name string, namespace string) *konfidencev1alpha1.Stage {
	stage := &konfidencev1alpha1.Stage{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	Expect(k8sClient.Create(ctx, stage)).To(Succeed())
	return stage
}

func GetStage(ctx context.Context, k8sClient client.Client, name, namespace string) *konfidencev1alpha1.Stage {
	stage := &konfidencev1alpha1.Stage{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, stage)
	Expect(err).ToNot(HaveOccurred(), "Failed to fetch stage: %s", name)
	return stage
}

func SetStageActiveVersion(ctx context.Context, k8sClient client.Client, stage *konfidencev1alpha1.Stage, stageVersionName string) {
	stage.Status.ActiveStageVersion = &konfidencev1alpha1.StageVersionReference{Name: stageVersionName}
	Expect(k8sClient.Status().Update(ctx, stage)).To(Succeed())
}

func CleanupStages(k8sClient client.Client) {
	ctx := context.Background()
	stages := &konfidencev1alpha1.StageList{}
	Expect(k8sClient.List(ctx, stages)).To(Succeed())
	for i := range stages.Items {
		s := &stages.Items[i]
		controllerutil.RemoveFinalizer(s, "konfidence.cloud/gateway-route-cleanup")
		_ = k8sClient.Update(ctx, s)
		_ = k8sClient.Delete(ctx, s)
	}
}

func CreateStageVersion(ctx context.Context, k8sClient client.Client, name, namespace, stageName, vector string) {
	stageVersion := &konfidencev1alpha1.StageVersion{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: konfidencev1alpha1.StageVersionSpec{
			Vector:          vector,
			StageGeneration: 1,
			StageRef:        &konfidencev1alpha1.StageReference{Name: stageName},
		},
	}
	Expect(k8sClient.Create(ctx, stageVersion)).To(Succeed())
}

func CleanupStageVersions(k8sClient client.Client) {
	ctx := context.Background()
	list := &konfidencev1alpha1.StageVersionList{}
	Expect(k8sClient.List(ctx, list)).To(Succeed())
	for i := range list.Items {
		_ = k8sClient.Delete(ctx, &list.Items[i])
	}
}

// CreateReadyLocalDeploymentTarget creates a "local" DeploymentTarget and marks it Ready so the activation executor
// resolves the in-cluster client.
func CreateReadyLocalDeploymentTarget(ctx context.Context, k8sClient client.Client, name, namespace, deploymentClassName string) {
	target := &konfidencev1alpha1.DeploymentTarget{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: konfidencev1alpha1.DeploymentTargetSpec{
			DeploymentClassName: deploymentClassName,
			Connection:          konfidencev1alpha1.DeploymentTargetConnection{Type: "local"},
		},
	}
	Expect(k8sClient.Create(ctx, target)).To(Succeed())
	meta.SetStatusCondition(&target.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.DeploymentTargetReadyCondition,
		Status:             metav1.ConditionTrue,
		Reason:             "Accepted",
		Message:            "test",
		LastTransitionTime: metav1.Now(),
	})
	Expect(k8sClient.Status().Update(ctx, target)).To(Succeed())
}

func CleanupDeploymentTargets(k8sClient client.Client) {
	ctx := context.Background()
	list := &konfidencev1alpha1.DeploymentTargetList{}
	Expect(k8sClient.List(ctx, list)).To(Succeed())
	for i := range list.Items {
		_ = k8sClient.Delete(ctx, &list.Items[i])
	}
}
