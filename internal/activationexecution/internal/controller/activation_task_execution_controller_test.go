package controller

import (
	"context"
	"fmt"
	"time"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	testutil "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/activationexecution/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
		testutil.CleanupVectorActivations(k8sClient)
		testutil.CleanupVectorDeployments(k8sClient)
		testutil.CleanupStages(k8sClient)
		testutil.CleanupStageVersions(k8sClient)
		testutil.CleanupDeploymentTargets(k8sClient)
	}

	BeforeEach(cleanup)
	AfterEach(cleanup)

	deploymentResult := func(name, k8sName string, port int32, ingressExposed bool) konfidencev1alpha1.DeploymentResult {
		return konfidencev1alpha1.DeploymentResult{
			Name: name,
			Type: "http-k8s-service",
			Spec: runtime.RawExtension{
				Raw: []byte(fmt.Sprintf(`{"Namespace":"%s","K8sName":"%s","ServicePorts":[{"name":"http","port":%d,"targetPort":"http"}],"IngressExposed":%t}`,
					Namespace, k8sName, port, ingressExposed)),
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
		It("builds route rules only for explicitly ingress-exposed deployment results", func() {
			exposed := deploymentResult(ServiceName1, ServiceName1, ServicePort1, true)
			unexposed := deploymentResult(ServiceName2, ServiceName2, ServicePort2, false)
			legacy := deploymentResult("legacy", "legacy", ServicePort1, false)
			legacy.Spec.Raw = []byte(fmt.Sprintf(
				`{"Namespace":"%s","K8sName":"legacy","ServicePorts":[{"name":"http","port":%d,"targetPort":"http"}]}`,
				Namespace, ServicePort1,
			))
			noPorts := deploymentResult("no-ports", "no-ports", ServicePort1, true)
			noPorts.Spec.Raw = []byte(fmt.Sprintf(
				`{"Namespace":"%s","K8sName":"no-ports","ServicePorts":[],"IngressExposed":true}`,
				Namespace,
			))
			malformed := deploymentResult("malformed", "malformed", ServicePort1, true)
			malformed.Spec.Raw = []byte(`{"IngressExposed":true`)
			otherType := deploymentResult("other-type", "other-type", ServicePort1, true)
			otherType.Type = "other"

			vectorDeployment := &konfidencev1alpha1.VectorDeployment{}
			vectorDeployment.Name = VectorDeployment
			vectorDeployment.Status.DeploymentResults = map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {exposed, unexposed, legacy},
				compB: {noPorts, malformed, otherType},
			}

			rules := buildRouteRules(Stage, vectorDeployment)

			Expect(rules).To(HaveLen(1))
			verifyRule(rules[0], Stage, ServiceName1, ServiceName1, ServicePort1, VectorDeployment)
		})

		It("creates a single per-stage HTTPRoute with a rule per ingress-exposed deployment result", func() {
			ctx := context.Background()
			testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")
			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, StageVersion, Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1, true)},
				compB: {
					deploymentResult(ServiceName2, ServiceName2, ServicePort2, true),
					deploymentResult("internal", "internal", ServicePort1, false),
				},
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
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")

			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, StageVersion, Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1, true)},
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
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort2, true)},
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

		It("removes the existing stage route when no result is ingress-exposed", func() {
			ctx := context.Background()
			stage := testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			controllerutil.AddFinalizer(stage, stageRouteFinalizer)
			Expect(k8sClient.Update(ctx, stage)).To(Succeed())
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")

			routeKey := types.NamespacedName{Name: StageRouteName(Stage), Namespace: Namespace}
			route := &gwapiv1.HTTPRoute{}
			route.Name = routeKey.Name
			route.Namespace = routeKey.Namespace
			route.Spec.ParentRefs = []gwapiv1.ParentReference{{Name: defaultGatewayName}}
			Expect(k8sClient.Create(ctx, route)).To(Succeed())

			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, StageVersion, Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1, false)},
			})
			testutil.CreateActivationTaskExecution(ctx, k8sClient, ActivationTaskExecution, Namespace,
				TypeGatewayRoute, ActivationTaskExecSpec, "vector-activation-001")

			Eventually(func() bool {
				err := k8sClient.Get(ctx, routeKey, &gwapiv1.HTTPRoute{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())

			ate := &konfidencev1alpha1.ActivationTaskExecution{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ActivationTaskExecution, Namespace: Namespace}, ate)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(ate.Status.Conditions, konfidencev1alpha1.ActivationTaskExecutionSucceeded)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			stage = testutil.GetStage(ctx, k8sClient, Stage, Namespace)
			Expect(controllerutil.ContainsFinalizer(stage, stageRouteFinalizer)).To(BeTrue())
		})

		It("succeeds when no route exists and no result is ingress-exposed", func() {
			ctx := context.Background()
			testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")
			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, StageVersion, Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1, false)},
			})

			testutil.CreateActivationTaskExecution(ctx, k8sClient, ActivationTaskExecution, Namespace,
				TypeGatewayRoute, ActivationTaskExecSpec, "vector-activation-001")

			ate := &konfidencev1alpha1.ActivationTaskExecution{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ActivationTaskExecution, Namespace: Namespace}, ate)).To(Succeed())
				g.Expect(meta.IsStatusConditionTrue(ate.Status.Conditions, konfidencev1alpha1.ActivationTaskExecutionSucceeded)).To(BeTrue())
				g.Expect(testutil.GetHttpRoutes(ctx, k8sClient).Items).To(BeEmpty())
			}, timeout, interval).Should(Succeed())
		})

		It("skips a superseded execution without touching the route", func() {
			ctx := context.Background()
			stage := testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")

			// older StageVersion first, then a strictly newer active one
			testutil.CreateStageVersion(ctx, k8sClient, "sv-old", Namespace, Stage, Vector001)
			time.Sleep(1100 * time.Millisecond)
			testutil.CreateStageVersion(ctx, k8sClient, "sv-new", Namespace, Stage, Vector001)
			testutil.SetStageActiveVersion(ctx, k8sClient, stage, "sv-new")

			testutil.CreateVectorActivation(ctx, k8sClient, "vector-activation-001", Namespace, Stage, "sv-old", Vector001, VectorDeployment)
			createVectorDeploymentWithResults(ctx, VectorDeployment, map[string]konfidencev1alpha1.ComponentDeploymentResults{
				compA: {deploymentResult(ServiceName1, ServiceName1, ServicePort1, true)},
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

	Context("When a Stage is deleted", func() {
		It("removes the per-stage route and releases the finalizer", func() {
			ctx := context.Background()
			stage := testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			controllerutil.AddFinalizer(stage, stageRouteFinalizer)
			Expect(k8sClient.Update(ctx, stage)).To(Succeed())
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")

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

		It("releases the finalizer when the per-stage route is already absent", func() {
			ctx := context.Background()
			stage := testutil.CreateStage(ctx, k8sClient, Stage, Namespace)
			controllerutil.AddFinalizer(stage, stageRouteFinalizer)
			Expect(k8sClient.Update(ctx, stage)).To(Succeed())
			testutil.CreateReadyLocalDeploymentTarget(ctx, k8sClient, "local-target", Namespace, "kubernetes-helm")

			Expect(k8sClient.Delete(ctx, stage)).To(Succeed())

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: Stage, Namespace: Namespace}, &konfidencev1alpha1.Stage{})
				return k8serrors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})
})

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
