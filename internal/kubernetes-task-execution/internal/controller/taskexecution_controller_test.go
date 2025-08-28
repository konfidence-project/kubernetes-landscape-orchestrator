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
	testutil "github.com/konfidence-project/landscape-kubernetes-task-execution-controller/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("TaskExecution Controller", func() {
	const (
		TaskExecution     = "task-execution-001"
		TaskExecutionType = "k8s"
		TaskExecutionSpec = ""
		Namespace         = "default"
		timeout           = time.Second * 10
		interval          = time.Millisecond * 250
	)

	BeforeEach(func() {
		testutil.CleanupTaskExecution(k8sClient, TaskExecution, Namespace)
	})

	AfterEach(func() {
		testutil.CleanupTaskExecution(k8sClient, TaskExecution, Namespace)
	})

	Context("When reconciling a taskExecution", func() {
		It("should successfully reconcile the taskExecution", func() {
			ctx := context.Background()
			testutil.CreateTaskExecution(ctx, k8sClient, TaskExecution, Namespace, TaskExecution, TaskExecutionType, nil, TaskExecutionSpec)

			// check that the taskExecution has been created and has valid properties
			taskExecution := &landscape.TaskExecution{}
			taskExecutionLookupKey := types.NamespacedName{Name: TaskExecution, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, taskExecutionLookupKey, taskExecution)).To(Succeed())
				g.Expect(taskExecution.Spec.Name).To(Equal(TaskExecution))
				g.Expect(taskExecution.Status.Conditions).To(HaveLen(1))
				g.Expect(taskExecution.Status.Conditions[0].Reason).To(Equal(landscape.TaskSucceeded))
				g.Expect(taskExecution.Status.Conditions[0].Type).To(Equal(landscape.TaskSucceeded))
				g.Expect(taskExecution.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			}, timeout, interval).Should(Succeed())
		})
	})
})
