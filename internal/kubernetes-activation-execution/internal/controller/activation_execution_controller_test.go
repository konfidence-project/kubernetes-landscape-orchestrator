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
)

var _ = Describe("ActivationExecution Controller", func() {
	const (
		ActivationExecution     = "activation-execution-001"
		ActivationExecutionType = "k8s-job"
		ActivationExecutionSpec = "{}"
		Namespace               = "default"
		timeout                 = time.Second * 10
		interval                = time.Millisecond * 250
	)

	BeforeEach(func() {
		testutil.CleanupActivationExecution(k8sClient, ActivationExecution, Namespace)
	})

	AfterEach(func() {
		testutil.CleanupActivationExecution(k8sClient, ActivationExecution, Namespace)
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

			// TODO implement first test

			// check that the activationExecution has been marked as successful
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, activationExecutionLookupKey, activationExecution)).To(Succeed())
				g.Expect(activationExecution.Status.Conditions).To(HaveLen(1))
				g.Expect(meta.IsStatusConditionTrue(activationExecution.Status.Conditions, landscape.ActivationExecutionSucceeded)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})
})
