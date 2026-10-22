package controller

import (
	"context"
	"fmt"
	"time"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	testutil "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/activationexecution/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ = Describe("ActivationTaskExecution Controller", func() {
	const (
		ActivationTaskExecution = "activation-execution-001"
		ActivationTaskExecSpec  = "{}"
		ServiceName1            = "candidates"
		ServicePort1            = 80
		ServiceName2            = "interviews"
		ServicePort2            = 81
		Namespace               = "default"
		Stage                   = "stage-dev"
		StageVersion            = "stage-version-stage-dev-068945876"
		Vector001               = "https://registry.kdenv.lab/ocm/vector//common.konfidence.cloud/example/vector:0.0.1"
		VectorDeployment        = "vector-deployment-001"
		compA                   = "comp-a"
		compB                   = "comp-b"
		timeout                 = time.Second * 10
		interval                = time.Millisecond * 250
	)

	cleanup := func() {
		testutil.CleanupActivationTaskExecution(k8sClient, ActivationTaskExecution, Namespace)
		testutil.CleanupHttpRoutes(k8sClient)
		testutil.CleanupGateways(k8sClient)
		testutil.CleanupVectorActivations(k8sClient)
		testutil.CleanupVectorDeployments(k8sClient)
		testutil.CleanupStages(k8sClient)
		testutil.CleanupStageVersions(k8sClient)
		testutil.CleanupDeploymentTargets(k8sClient)
	}

	BeforeEach(cleanup)
	AfterEach(cleanup)

	deploymentResult := func(name, k8sName string, port int32) konfidencev1alpha1.DeploymentResult {
		return konfidencev1alpha1.DeploymentResult{
			Name: name,
			Type: "http-k8s-service",
			Spec: runtime.RawExtension{
				Raw: []byte(fmt.Sprintf(`{"Namespace":"%s","K8sName":"%s","ServicePorts":[{"name":"http","port":%d,"targetPort":"http"}]}`,
					Namespace, k8sName, port)),
			},
		}
	}

	createVectorDeploymentWithResults := func(ctx context.Context, name string, results map[string]konfidencev1alpha1.ComponentDeploymentResults) {
		testutil.CreateVectorDeployment(ctx, k8sClient, name, Namespace, Vector001)
		vd := &konfidencev1alpha1.VectorDeployment{}
		key := types.NamespacedName{Name: name, Namespace: Namespace}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, key, vd)).To(Succeed())
		}, timeout, interval).Should(Succeed())
		vd.Status.DeploymentResults = results
		testutil.UpdateVectorDeploymentStatus(ctx, k8sClient, vd)
	}

	Context("When reconciling a gateway-route activation", func() {
		It("creates a single per-stage HTTPRoute with a rule per deployment result", func() {
			ctx := context.Background()
			testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "helm.konfidence.cloud")
			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, StageVersion, Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1)},
				compB: {deploymentResult(ServiceName2, ServiceName2, ServicePort2)},
			})

			testutil.CreateActivationTaskExecution(ctx, k8sClient, ActivationTaskExecution, Namespace,
				TypeGatewayRoute, ActivationTaskExecSpec, "vector-activation-001")

			route := &gwapiv1.HTTPRoute{}
			routeKey := types.NamespacedName{Name: StageRouteName(Stage), Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routeKey, route)).To(Succeed())
				g.Expect(route.Spec.ParentRefs).To(HaveLen(1))
				g.Expect(route.Spec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName(defaultGatewayName)))
				g.Expect(route.Spec.ParentRefs[0].Namespace).To(BeNil())
				g.Expect(route.Spec.Hostnames).To(BeEmpty())
				g.Expect(route.GetOwnerReferences()).To(BeEmpty())
				g.Expect(route.Spec.Rules).To(HaveLen(2))
			}, timeout, interval).Should(Succeed())

			// rules are sorted by deployment-result name: candidates, interviews
			verifyRule(route.Spec.Rules[0], Stage, ServiceName1, ServiceName1, ServicePort1, VectorDeployment)
			verifyRule(route.Spec.Rules[1], Stage, ServiceName2, ServiceName2, ServicePort2, VectorDeployment)

			ate := &konfidencev1alpha1.ActivationTaskExecution{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ActivationTaskExecution, Namespace: Namespace}, ate)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(ate.Status.Conditions, konfidencev1alpha1.ActivationTaskExecutionSucceeded)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			// the stage carries the cleanup finalizer
			Eventually(func(g Gomega) {
				stage := testutil.GetStage(ctx, k8sClient, Stage, Namespace)
				g.Expect(controllerutil.ContainsFinalizer(stage, stageRouteFinalizer)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})

		It("patches the existing per-stage route in place on a flip", func() {
			ctx := context.Background()
			testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "helm.konfidence.cloud")

			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, StageVersion, Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1)},
			})
			testutil.CreateActivationTaskExecution(ctx, k8sClient, ActivationTaskExecution, Namespace,
				TypeGatewayRoute, ActivationTaskExecSpec, "vector-activation-001")

			routeKey := types.NamespacedName{Name: StageRouteName(Stage), Namespace: Namespace}
			route := &gwapiv1.HTTPRoute{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routeKey, route)).To(Succeed())
				g.Expect(route.Spec.Rules).To(HaveLen(1))
				g.Expect(route.Spec.Rules[0].Filters[0].RequestHeaderModifier.Set[0].Value).To(Equal(VectorDeployment))
			}, timeout, interval).Should(Succeed())

			// flip: new VD + new activation + new execution
			const vd2 = "vector-deployment-002"
			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-002", Namespace, Stage, StageVersion+"-2", Vector001, vd2)
			createVectorDeploymentWithResults(ctx, vd2, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort2)},
			})
			testutil.CreateActivationTaskExecution(ctx, k8sClient, "activation-execution-002", Namespace,
				TypeGatewayRoute, ActivationTaskExecSpec, "vector-activation-002")

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routeKey, route)).To(Succeed())
				g.Expect(route.Spec.Rules).To(HaveLen(1))
				g.Expect(route.Spec.Rules[0].Filters[0].RequestHeaderModifier.Set[0].Value).To(Equal(vd2))
				g.Expect(*route.Spec.Rules[0].BackendRefs[0].Port).To(Equal(gwapiv1.PortNumber(ServicePort2)))
			}, timeout, interval).Should(Succeed())

			// still exactly one route for the stage
			routes := testutil.GetHttpRoutes(ctx, k8sClient)
			Expect(routes.Items).To(HaveLen(1))
			testutil.CleanupActivationTaskExecution(k8sClient, "activation-execution-002", Namespace)
		})

		It("skips a superseded execution without touching the route", func() {
			ctx := context.Background()
			stage := testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "helm.konfidence.cloud")

			// older StageVersion first, then a strictly newer active one
			testutil.CreateStageVersion(ctx, k8sClient, "sv-old", Namespace, Stage, Vector001)
			time.Sleep(1100 * time.Millisecond)
			testutil.CreateStageVersion(ctx, k8sClient, "sv-new", Namespace, Stage, Vector001)
			testutil.SetStageActiveVersion(ctx, k8sClient, stage, "sv-new")

			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, "sv-old", Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1)},
			})
			testutil.CreateActivationTaskExecution(ctx, k8sClient, ActivationTaskExecution, Namespace,
				TypeGatewayRoute, ActivationTaskExecSpec, "vector-activation-001")

			ate := &konfidencev1alpha1.ActivationTaskExecution{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ActivationTaskExecution, Namespace: Namespace}, ate)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(ate.Status.Conditions, konfidencev1alpha1.ActivationTaskExecutionSucceeded)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			Consistently(func(g Gomega) {
				g.Expect(testutil.GetHttpRoutes(ctx, k8sClient).Items).To(BeEmpty())
			}, time.Second*2, interval).Should(Succeed())
		})
	})

	Context("When the registration sets a gatewayClassName", func() {
		const provisionSpec = `{"gatewayClassName":"eg"}`

		routerKey := types.NamespacedName{Name: defaultGatewayName, Namespace: Namespace}
		routeKey := types.NamespacedName{Name: StageRouteName(Stage), Namespace: Namespace}
		ateKey := types.NamespacedName{Name: ActivationTaskExecution, Namespace: Namespace}

		BeforeEach(func() {
			testutil.CreateGatewayClass(context.Background(), k8sClient, "eg")
			DeferCleanup(testutil.CleanupGatewayClasses, k8sClient)
		})

		activate := func(ctx context.Context, spec string) {
			testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")
			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, StageVersion, Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1)},
			})
			testutil.CreateActivationTaskExecution(ctx, k8sClient, ActivationTaskExecution, Namespace, TypeGatewayRoute, spec, "vector-activation-001")
		}

		eventuallyRouter := func(ctx context.Context) *gwapiv1.Gateway {
			gateway := &gwapiv1.Gateway{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routerKey, gateway)).To(Succeed())
			}, timeout, interval).Should(Succeed())
			return gateway
		}

		eventuallyCondition := func(ctx context.Context, conditionType, reason string) *metav1.Condition {
			var condition *metav1.Condition
			Eventually(func(g Gomega) {
				ate := &konfidencev1alpha1.ActivationTaskExecution{}
				g.Expect(k8sClient.Get(ctx, ateKey, ate)).To(Succeed())
				condition = meta.FindStatusCondition(ate.Status.Conditions, conditionType)
				g.Expect(condition).ToNot(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(condition.Reason).To(Equal(reason))
			}, routerGatewayPollInterval+timeout, interval).Should(Succeed())
			return condition
		}

		expectNoRoute := func(ctx context.Context) {
			Consistently(func(g Gomega) {
				g.Expect(testutil.GetHttpRoutes(ctx, k8sClient).Items).To(BeEmpty())
			}, time.Second*2, interval).Should(Succeed())
		}

		clusterAddress := func(value string) gwapiv1.GatewayStatusAddress {
			return gwapiv1.GatewayStatusAddress{Value: value, Routability: ptrTo(gwapiv1.GatewayAddressRoutabilityCluster)}
		}

		It("reports InProgress while waiting and attaches the route once the address is cluster-only", func() {
			ctx := context.Background()
			activate(ctx, provisionSpec)

			gateway := eventuallyRouter(ctx)
			Expect(gateway.Labels).To(HaveKeyWithValue(labelProvisionedBy, provisionedByOrchestrator))
			Expect(gateway.Spec.GatewayClassName).To(Equal(gwapiv1.ObjectName("eg")))
			Expect(gateway.Spec.Addresses).To(HaveLen(1))
			Expect(gateway.Spec.Addresses[0].Routability).To(Equal(gwapiv1.GatewayAddressRoutabilityCluster))
			Expect(gateway.Spec.Listeners).To(HaveLen(1))
			Expect(gateway.Spec.Listeners[0].Protocol).To(Equal(gwapiv1.HTTPProtocolType))

			waiting := eventuallyCondition(ctx, konfidencev1alpha1.ActivationTaskExecutionInProgress, reasonWaitingForClusterAddress)
			Expect(waiting.Message).To(ContainSubstring(defaultGatewayName))
			expectNoRoute(ctx)

			testutil.SetGatewayStatus(ctx, k8sClient, gateway, nil, clusterAddress("10.96.0.10"))

			Eventually(func(g Gomega) {
				route := &gwapiv1.HTTPRoute{}
				g.Expect(k8sClient.Get(ctx, routeKey, route)).To(Succeed())
				g.Expect(route.Spec.ParentRefs).To(HaveLen(1))
				g.Expect(route.Spec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName(defaultGatewayName)))
			}, routerGatewayPollInterval+timeout, interval).Should(Succeed())
			eventuallyCondition(ctx, konfidencev1alpha1.ActivationTaskExecutionSucceeded, konfidencev1alpha1.ActivationTaskExecutionSucceeded)

			ate := &konfidencev1alpha1.ActivationTaskExecution{}
			Expect(k8sClient.Get(ctx, ateKey, ate)).To(Succeed())
			Expect(meta.FindStatusCondition(ate.Status.Conditions, konfidencev1alpha1.ActivationTaskExecutionInProgress)).To(BeNil())
		})

		It("fails the execution and deletes the Gateway when the implementation programs a non-cluster address", func() {
			ctx := context.Background()
			activate(ctx, provisionSpec)

			gateway := eventuallyRouter(ctx)
			testutil.SetGatewayStatus(ctx, k8sClient, gateway,
				[]metav1.Condition{gatewayCondition(gwapiv1.GatewayConditionProgrammed, metav1.ConditionTrue, "Programmed", "")},
				clusterAddress("10.96.0.10"),
				gwapiv1.GatewayStatusAddress{Value: publicAddress},
			)

			failed := eventuallyCondition(ctx, konfidencev1alpha1.ActivationTaskExecutionFailed, reasonAddressNotClusterRoutable)
			Expect(failed.Message).To(ContainSubstring(publicAddress))
			Eventually(func(g Gomega) {
				g.Expect(testutil.GetGateways(ctx, k8sClient).Items).To(BeEmpty())
			}, timeout, interval).Should(Succeed())
			expectNoRoute(ctx)
		})

		It("fails the execution with the implementation's reason when the Gateway is not accepted", func() {
			ctx := context.Background()
			activate(ctx, provisionSpec)

			gateway := eventuallyRouter(ctx)
			testutil.SetGatewayStatus(ctx, k8sClient, gateway,
				[]metav1.Condition{gatewayCondition(gwapiv1.GatewayConditionAccepted, metav1.ConditionFalse, "InvalidParameters", "listener port 80 is reserved")},
			)

			failed := eventuallyCondition(ctx, konfidencev1alpha1.ActivationTaskExecutionFailed, reasonGatewayNotAccepted)
			Expect(failed.Message).To(ContainSubstring("listener port 80 is reserved"))
			expectNoRoute(ctx)
		})

		It("reports a missing GatewayClass without creating a Gateway", func() {
			ctx := context.Background()
			activate(ctx, `{"gatewayClassName":"missing"}`)

			waiting := eventuallyCondition(ctx, konfidencev1alpha1.ActivationTaskExecutionInProgress, reasonGatewayClassNotFound)
			Expect(waiting.Message).To(ContainSubstring("missing"))
			Expect(testutil.GetGateways(ctx, k8sClient).Items).To(BeEmpty())
			expectNoRoute(ctx)
		})

		It("uses an admin-provided konfidence-router without modifying it, even with a generic managed-by label", func() {
			ctx := context.Background()
			admin := &gwapiv1.Gateway{}
			admin.Name = defaultGatewayName
			admin.Namespace = Namespace
			admin.Labels = map[string]string{"app.kubernetes.io/managed-by": "konfidence"}
			admin.Spec.GatewayClassName = "admin-class"
			admin.Spec.Listeners = []gwapiv1.Listener{{Name: "http", Protocol: gwapiv1.HTTPProtocolType, Port: 80}}
			Expect(k8sClient.Create(ctx, admin)).To(Succeed())

			activate(ctx, provisionSpec)

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routeKey, &gwapiv1.HTTPRoute{})).To(Succeed())
			}, timeout, interval).Should(Succeed())

			gateway := eventuallyRouter(ctx)
			Expect(gateway.Labels).ToNot(HaveKey(labelProvisionedBy))
			Expect(gateway.Spec.GatewayClassName).To(Equal(gwapiv1.ObjectName("admin-class")))
			Expect(gateway.Spec.Addresses).To(BeEmpty())
		})

		It("never provisions a Gateway when the registration names one", func() {
			ctx := context.Background()
			activate(ctx, `{"gatewayClassName":"eg","gatewayName":"internal","gatewayNamespace":"edge"}`)

			route := &gwapiv1.HTTPRoute{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, routeKey, route)).To(Succeed())
			}, timeout, interval).Should(Succeed())
			Expect(route.Spec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName("internal")))
			Expect(testutil.GetGateways(ctx, k8sClient).Items).To(BeEmpty())
		})
	})

	DescribeTable("evaluateRouterGateway",
		func(gateway *gwapiv1.Gateway, age time.Duration, phase routerGatewayPhase, reason string) {
			now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
			gateway.CreationTimestamp = metav1.NewTime(now.Add(-age))
			state := evaluateRouterGateway(gateway, now)
			Expect(state.phase).To(Equal(phase))
			Expect(state.reason).To(Equal(reason))
			Expect(state.exposed).To(Equal(reason == reasonRoutabilityNotPersisted || reason == reasonAddressNotClusterRoutable))
			if phase != routerGatewayReady {
				Expect(state.message).ToNot(BeEmpty())
			}
		},
		Entry("pruned spec routability (standard-channel CRDs)",
			routerGateway(nil, nil, []gwapiv1.GatewaySpecAddress{{}}),
			time.Second, routerGatewayFailed, reasonRoutabilityNotPersisted),
		Entry("Gateway rejected by the implementation",
			routerGateway([]metav1.Condition{gatewayCondition(gwapiv1.GatewayConditionAccepted, metav1.ConditionFalse, "InvalidParameters", "bad")}, nil, nil),
			time.Second, routerGatewayFailed, reasonGatewayNotAccepted),
		Entry("no status address assigned yet",
			routerGateway(nil, nil, nil),
			time.Second, routerGatewayPending, reasonWaitingForClusterAddress),
		Entry("public address that is not programmed yet",
			routerGateway(nil, []gwapiv1.GatewayStatusAddress{{Value: publicAddress}}, nil),
			time.Second, routerGatewayPending, reasonWaitingForClusterAddress),
		Entry("programmed with an address that ignores routability",
			routerGateway([]metav1.Condition{gatewayCondition(gwapiv1.GatewayConditionProgrammed, metav1.ConditionTrue, "Programmed", "")},
				[]gwapiv1.GatewayStatusAddress{{Value: publicAddress}}, nil),
			time.Second, routerGatewayFailed, reasonAddressNotClusterRoutable),
		Entry("no address after the readiness timeout (no controller serves the class)",
			routerGateway(nil, nil, nil),
			routerGatewayReadyTimeout+time.Second, routerGatewayFailed, reasonGatewayNotReady),
		Entry("every status address is cluster-routable",
			routerGateway(nil, []gwapiv1.GatewayStatusAddress{{Value: "10.96.0.10", Routability: ptrTo(gwapiv1.GatewayAddressRoutabilityCluster)}}, nil),
			time.Second, routerGatewayReady, ""),
	)

	DescribeTable("missingGatewayClassState",
		func(waited time.Duration, phase routerGatewayPhase) {
			now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
			state := missingGatewayClassState("eg", now.Add(-waited), now)
			Expect(state.phase).To(Equal(phase))
			Expect(state.reason).To(Equal(reasonGatewayClassNotFound))
			Expect(state.message).To(ContainSubstring("eg"))
		},
		Entry("within the readiness timeout", time.Second, routerGatewayPending),
		Entry("after the readiness timeout", routerGatewayReadyTimeout+time.Second, routerGatewayFailed),
	)

	Context("When a Stage is deleted", func() {
		It("removes the per-stage route and releases the finalizer", func() {
			ctx := context.Background()
			stage := testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			controllerutil.AddFinalizer(stage, stageRouteFinalizer)
			Expect(k8sClient.Update(ctx, stage)).To(Succeed())
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "helm.konfidence.cloud")

			route := &gwapiv1.HTTPRoute{}
			route.Name = StageRouteName(Stage)
			route.Namespace = Namespace
			route.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: defaultGatewayName}}
			Expect(k8sClient.Create(ctx, route)).To(Succeed())

			Expect(k8sClient.Delete(ctx, stage)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: StageRouteName(Stage), Namespace: Namespace}, &gwapiv1.HTTPRoute{})
				g.Expect(err).To(HaveOccurred())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: Stage, Namespace: Namespace}, &konfidencev1alpha1.Stage{})
				g.Expect(err).To(HaveOccurred())
			}, timeout, interval).Should(Succeed())
		})
	})
})

// publicAddress is a TEST-NET-3 address standing in for an externally reachable Gateway address.
const publicAddress = "203.0.113.7"

func ptrTo[T any](v T) *T { return &v }

func gatewayCondition(conditionType gwapiv1.GatewayConditionType, status metav1.ConditionStatus, reason, message string) metav1.Condition {
	return metav1.Condition{
		Type:               string(conditionType),
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	}
}

// routerGateway builds a provisioned konfidence-router; nil spec addresses mean the request persisted.
func routerGateway(conditions []metav1.Condition, addresses []gwapiv1.GatewayStatusAddress, specAddresses []gwapiv1.GatewaySpecAddress) *gwapiv1.Gateway {
	gateway := newRouterGateway("default", "eg")
	if specAddresses != nil {
		gateway.Spec.Addresses = specAddresses
	}
	gateway.Status.Conditions = conditions
	gateway.Status.Addresses = addresses
	return gateway
}

func verifyRule(rule gwapiv1.HTTPRouteRule, stage, resultName, serviceName string, port gwapiv1.PortNumber, vectorDeployment string) {
	// Header-only selection: stage + service. Any path is the API-defaulted catch-all "/".
	Expect(rule.Matches).To(HaveLen(1))
	if rule.Matches[0].Path != nil {
		Expect(*rule.Matches[0].Path.Value).To(Equal("/"))
	}
	Expect(rule.Matches[0].Headers).To(HaveLen(2))
	Expect(rule.Matches[0].Headers[0].Name).To(Equal(gwapiv1.HTTPHeaderName(headerStage)))
	Expect(rule.Matches[0].Headers[0].Value).To(Equal(stage))
	Expect(rule.Matches[0].Headers[1].Name).To(Equal(gwapiv1.HTTPHeaderName(headerService)))
	Expect(rule.Matches[0].Headers[1].Value).To(Equal(resultName))

	Expect(rule.Filters).To(HaveLen(1))
	hm := rule.Filters[0].RequestHeaderModifier
	Expect(hm.Set).To(HaveLen(1))
	Expect(hm.Set[0].Name).To(Equal(gwapiv1.HTTPHeaderName(headerRoutingKey)))
	Expect(hm.Set[0].Value).To(Equal(vectorDeployment))
	Expect(hm.Remove).To(ConsistOf(headerStage, headerService))

	Expect(rule.BackendRefs).To(HaveLen(1))
	Expect(rule.BackendRefs[0].Name).To(Equal(gwapiv1.ObjectName(serviceName)))
	Expect(*rule.BackendRefs[0].Port).To(Equal(port))
}
