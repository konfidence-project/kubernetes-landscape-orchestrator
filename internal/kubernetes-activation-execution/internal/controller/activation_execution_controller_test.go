/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"time"

	landscape "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	testutil "github.com/konfidence-project/landscape-kubernetes-activation-execution-controller/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

var _ = Describe("ActivationExecution Controller", func() {
	const (
		ActivationExecution     = "activation-execution-001"
		ActivationExecutionType = "k8s-job"
		ActivationExecutionSpec = "{}"
		HttpRouteName           = "example-http-route"
		GatewayName             = "example-gateway"
		HostName                = "example-host"
		VectorId                = "vector-123"
		ServiceName             = "example-service"
		ServicePort             = 80
		Namespace               = "default"
		timeout                 = time.Second * 10
		interval                = time.Millisecond * 250
	)

	BeforeEach(func() {
		testutil.CleanupActivationExecution(k8sClient, ActivationExecution, Namespace)
		testutil.CleanupHttpRoutes(k8sClient)
	})

	AfterEach(func() {
		testutil.CleanupActivationExecution(k8sClient, ActivationExecution, Namespace)
		testutil.CleanupHttpRoutes(k8sClient)
	})

	Context("When reconciling a activationExecution", func() {
		It("should successfully reconcile the activationExecution", func() {
			ctx := context.Background()
			testutil.CreateActivationExecution(ctx, k8sClient, ActivationExecution, Namespace, ActivationExecution, ActivationExecutionType, ActivationExecutionSpec)

			// check that the activationExecution has been created and has valid properties
			activationExecution := &landscape.ActivationExecution{}
			activationExecutionLookupKey := types.NamespacedName{Name: ActivationExecution, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, activationExecutionLookupKey, activationExecution)).To(Succeed())
				g.Expect(activationExecution.Spec.Name).To(Equal(ActivationExecution))
				g.Expect(activationExecution.Spec.Type).To(Equal(ActivationExecutionType))
			}, timeout, interval).Should(Succeed())

			// check that the httpRoute has been created and has valid properties
			headerMatchType := gwapiv1.HeaderMatchExact
			port := int32(ServicePort)
			httpRoute := &gwapiv1.HTTPRoute{}
			httpRouteLookupKey := types.NamespacedName{Name: HttpRouteName, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, httpRouteLookupKey, httpRoute)).To(Succeed())
				g.Expect(httpRoute.Spec.CommonRouteSpec.ParentRefs).To(HaveLen(1))
				g.Expect(httpRoute.Spec.CommonRouteSpec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName(GatewayName)))
				g.Expect(httpRoute.Spec.Hostnames).To(HaveLen(1))
				g.Expect(httpRoute.Spec.Hostnames[0]).To(Equal(gwapiv1.Hostname(HostName)))
				g.Expect(httpRoute.Spec.Rules).To(HaveLen(1))
				g.Expect(httpRoute.Spec.Rules[0].Matches).To(HaveLen(1))
				g.Expect(httpRoute.Spec.Rules[0].Matches[0].Headers).To(HaveLen(1))
				g.Expect(httpRoute.Spec.Rules[0].Matches[0].Headers[0].Name).To(Equal(gwapiv1.HTTPHeaderName(XVectorId)))
				g.Expect(httpRoute.Spec.Rules[0].Matches[0].Headers[0].Value).To(Equal(VectorId))
				g.Expect(httpRoute.Spec.Rules[0].Matches[0].Headers[0].Type).To(Equal(&headerMatchType))
				g.Expect(httpRoute.Spec.Rules[0].BackendRefs).To(HaveLen(1))
				g.Expect(httpRoute.Spec.Rules[0].BackendRefs[0].BackendRef.Name).To(Equal(gwapiv1.ObjectName(ServiceName)))
				g.Expect(httpRoute.Spec.Rules[0].BackendRefs[0].BackendRef.Port).To(Equal(&port))
			}, timeout, interval).Should(Succeed())

			// check that the activationExecution has been marked as successful
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, activationExecutionLookupKey, activationExecution)).To(Succeed())
				g.Expect(activationExecution.Status.Conditions).To(HaveLen(1))
				g.Expect(meta.IsStatusConditionTrue(activationExecution.Status.Conditions, landscape.ActivationExecutionSucceeded)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})
})
