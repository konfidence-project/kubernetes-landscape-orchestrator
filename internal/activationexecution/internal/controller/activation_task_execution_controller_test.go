package controller

import (
	"context"
	"fmt"
	"time"

	landscape "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	testutil "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/activationexecution/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ = Describe("ActivationTaskExecution Controller", func() {
	const (
		ActivationTaskExecution     = "activation-execution-001"
		ActivationTaskExecutionType = HttpActivationTaskExecutionType
		ActivationTaskExecutionSpec = "{}"
		GatewayName                 = Gateway
		ServiceName1                = "example-service-1"
		ServicePort1                = 80
		ServiceName2                = "example-service-2"
		ServicePort2                = 81
		Namespace                   = "default"
		Stage                       = "stage-dev"
		StageVersion                = "stage-version-stage-dev-068945876"
		VectorActivation            = "vector-activation-001"
		Vector001                   = "https://registry.kdenv.lab/ocm/vector//common.konfidence.cloud/example/vector:0.0.1"
		VectorDeployment            = "vector-deployment-001"
		HttpRouteName1              = ServiceName1 + "-" + VectorDeployment + "-" + "activation"
		HttpRouteName2              = ServiceName2 + "-" + VectorDeployment + "-" + "activation"
		HostName1                   = ServiceName1 + "." + Stage + "." + DefaultDomain
		HostName2                   = ServiceName2 + "." + Stage + "." + DefaultDomain
		timeout                     = time.Second * 10
		interval                    = time.Millisecond * 250
	)

	BeforeEach(func() {
		testutil.CleanupActivationTaskExecution(k8sClient, ActivationTaskExecution, Namespace)
		testutil.CleanupHttpRoutes(k8sClient)
		testutil.CleanupVectorActivations(k8sClient)
		testutil.CleanupVectorDeployments(k8sClient)
	})

	AfterEach(func() {
		testutil.CleanupActivationTaskExecution(k8sClient, ActivationTaskExecution, Namespace)
		testutil.CleanupHttpRoutes(k8sClient)
		testutil.CleanupVectorActivations(k8sClient)
		testutil.CleanupVectorDeployments(k8sClient)
	})

	Context("When reconciling a activationTaskExecution", func() {
		It("should successfully reconcile the activationTaskExecution", func() {
			ctx := context.Background()
			testutil.CreateVectorActivation(ctx, k8sClient, VectorActivation, Namespace, Stage, StageVersion, Vector001, VectorDeployment)

			// check that the vectorActivation has been created and has valid properties
			vectorActivation := &landscape.VectorActivation{}
			vectorActivationLookupKey := types.NamespacedName{Name: VectorActivation, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, vectorActivationLookupKey, vectorActivation)).To(Succeed())
				g.Expect(vectorActivation.Name).To(Equal(VectorActivation))
			}, timeout, interval).Should(Succeed())

			testutil.CreateVectorDeployment(ctx, k8sClient, VectorDeployment, Namespace, Vector001)

			// check that the vectorDeployment has been created and has valid properties
			vectorDeployment := &landscape.VectorDeployment{}
			vectorDeploymentLookupKey := types.NamespacedName{Name: VectorDeployment, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, vectorDeploymentLookupKey, vectorDeployment)).To(Succeed())
				g.Expect(vectorDeployment.Name).To(Equal(VectorDeployment))
			}, timeout, interval).Should(Succeed())

			// create deploymentResults
			deploymentResults := make(map[string]landscape.DeploymentResult)
			deploymentResults[ServiceName1] = landscape.DeploymentResult{
				Name: ServiceName1,
				Type: HttpActivationTaskExecutionType,
				Spec: runtime.RawExtension{
					Raw: []byte(fmt.Sprintf("{\"K8sName\": \"%s\", \"servicePorts\": [{\"name\": \"%s\", \"port\": %d, \"targetPort\": \"http\"}]}", ServiceName1, HttpRouteName1, ServicePort1)),
				},
			}

			deploymentResults[ServiceName2] = landscape.DeploymentResult{
				Name: ServiceName2,
				Type: HttpActivationTaskExecutionType,
				Spec: runtime.RawExtension{
					Raw: []byte(fmt.Sprintf("{\"K8sName\": \"%s\", \"servicePorts\": [{\"name\": \"%s\", \"port\": %d, \"targetPort\": \"http\"}]}", ServiceName2, HttpRouteName2, ServicePort2)),
				},
			}

			// and update status
			vectorDeployment.Status.DeploymentResults = deploymentResults
			testutil.UpdateVectorDeploymentStatus(ctx, k8sClient, vectorDeployment)

			// check that the status has actually been updated
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, vectorDeploymentLookupKey, vectorDeployment)).To(Succeed())
				g.Expect(vectorDeployment.Status.DeploymentResults).To(HaveLen(2))
			}, timeout, interval).Should(Succeed())

			testutil.CreateActivationTaskExecution(ctx, k8sClient, ActivationTaskExecution, Namespace, ActivationTaskExecutionType, ActivationTaskExecutionSpec, VectorActivation)

			// check that the activationTaskExecution has been created and has valid properties
			activationTaskExecution := &landscape.ActivationTaskExecution{}
			activationTaskExecutionLookupKey := types.NamespacedName{Name: ActivationTaskExecution, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, activationTaskExecutionLookupKey, activationTaskExecution)).To(Succeed())
				g.Expect(activationTaskExecution.Name).To(Equal(ActivationTaskExecution))
				g.Expect(activationTaskExecution.Spec.Type).To(Equal(ActivationTaskExecutionType))
				g.Expect(activationTaskExecution.Spec.VectorActivation).To(Equal(VectorActivation))
			}, timeout, interval).Should(Succeed())

			// check that the httpRoutes have been created and have valid properties
			port1 := int32(ServicePort1)
			httpRoute1 := &gwapiv1.HTTPRoute{}
			httpRouteLookupKey1 := types.NamespacedName{Name: HttpRouteName1, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, httpRouteLookupKey1, httpRoute1)).To(Succeed())
				g.Expect(httpRoute1.Spec.CommonRouteSpec.ParentRefs).To(HaveLen(1))
				g.Expect(httpRoute1.Spec.CommonRouteSpec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName(GatewayName)))
				g.Expect(httpRoute1.Spec.Hostnames).To(HaveLen(1))
				g.Expect(httpRoute1.Spec.Hostnames[0]).To(Equal(gwapiv1.Hostname(HostName1)))
				g.Expect(httpRoute1.Spec.Rules).To(HaveLen(1))
				g.Expect(httpRoute1.Spec.Rules[0].Filters).To(HaveLen(1))
				g.Expect(httpRoute1.Spec.Rules[0].Filters[0].Type).To(Equal(gwapiv1.HTTPRouteFilterRequestHeaderModifier))
				g.Expect(httpRoute1.Spec.Rules[0].Filters[0].RequestHeaderModifier).To(Not(BeNil()))
				g.Expect(httpRoute1.Spec.Rules[0].Filters[0].RequestHeaderModifier.Add).To(HaveLen(1))
				g.Expect(httpRoute1.Spec.Rules[0].Filters[0].RequestHeaderModifier.Add[0].Name).To(Equal(gwapiv1.HTTPHeaderName(XVectorId)))
				g.Expect(httpRoute1.Spec.Rules[0].Filters[0].RequestHeaderModifier.Add[0].Value).To(Equal(VectorDeployment))
				g.Expect(httpRoute1.Spec.Rules[0].BackendRefs).To(HaveLen(1))
				g.Expect(httpRoute1.Spec.Rules[0].BackendRefs[0].BackendRef.Name).To(Equal(gwapiv1.ObjectName(ServiceName1)))
				g.Expect(httpRoute1.Spec.Rules[0].BackendRefs[0].BackendRef.Port).To(Equal(&port1))
				g.Expect(httpRoute1.GetOwnerReferences()).To(HaveLen(1))
				g.Expect(testutil.HasOwnerReference(httpRoute1.GetOwnerReferences(), metav1.OwnerReference{
					Kind: landscape.VectorActivationKind,
					Name: VectorActivation,
				})).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			httpRoute2 := &gwapiv1.HTTPRoute{}
			httpRouteLookupKey2 := types.NamespacedName{Name: HttpRouteName2, Namespace: Namespace}
			port2 := int32(ServicePort2)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, httpRouteLookupKey2, httpRoute2)).To(Succeed())
				g.Expect(httpRoute2.Spec.CommonRouteSpec.ParentRefs).To(HaveLen(1))
				g.Expect(httpRoute2.Spec.CommonRouteSpec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName(GatewayName)))
				g.Expect(httpRoute2.Spec.Hostnames).To(HaveLen(1))
				g.Expect(httpRoute2.Spec.Hostnames[0]).To(Equal(gwapiv1.Hostname(HostName2)))
				g.Expect(httpRoute2.Spec.Rules).To(HaveLen(1))
				g.Expect(httpRoute2.Spec.Rules[0].Filters).To(HaveLen(1))
				g.Expect(httpRoute2.Spec.Rules[0].Filters[0].Type).To(Equal(gwapiv1.HTTPRouteFilterRequestHeaderModifier))
				g.Expect(httpRoute2.Spec.Rules[0].Filters[0].RequestHeaderModifier).To(Not(BeNil()))
				g.Expect(httpRoute2.Spec.Rules[0].Filters[0].RequestHeaderModifier.Add).To(HaveLen(1))
				g.Expect(httpRoute2.Spec.Rules[0].Filters[0].RequestHeaderModifier.Add[0].Name).To(Equal(gwapiv1.HTTPHeaderName(XVectorId)))
				g.Expect(httpRoute2.Spec.Rules[0].Filters[0].RequestHeaderModifier.Add[0].Value).To(Equal(VectorDeployment))
				g.Expect(httpRoute2.Spec.Rules[0].BackendRefs).To(HaveLen(1))
				g.Expect(httpRoute2.Spec.Rules[0].BackendRefs[0].BackendRef.Name).To(Equal(gwapiv1.ObjectName(ServiceName2)))
				g.Expect(httpRoute2.Spec.Rules[0].BackendRefs[0].BackendRef.Port).To(Equal(&port2))
				g.Expect(httpRoute2.GetOwnerReferences()).To(HaveLen(1))
				g.Expect(testutil.HasOwnerReference(httpRoute2.GetOwnerReferences(), metav1.OwnerReference{
					Kind: landscape.VectorActivationKind,
					Name: VectorActivation,
				})).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			// check that the activationTaskExecution has been marked as successful
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, activationTaskExecutionLookupKey, activationTaskExecution)).To(Succeed())
				g.Expect(activationTaskExecution.Status.Conditions).To(HaveLen(1))
				g.Expect(meta.IsStatusConditionTrue(activationTaskExecution.Status.Conditions, landscape.ActivationTaskExecutionSucceeded)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})
})
