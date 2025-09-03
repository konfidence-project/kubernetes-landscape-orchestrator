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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("TaskExecution Controller", func() {
	const (
		TaskExecution     = "task-execution-001"
		TaskExecutionType = "k8s-job"
		TaskExecutionSpec = "{\"template\": {\"spec\": {\"containers\": [ {\"name\": \"task-execution-001\",\"image\": \"busybox\",\"command\": [\"echo\",\"I am task 1 of service 1\"]}],\"restartPolicy\": \"Never\"}},\"backoffLimit\": 4}"
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
			}, timeout, interval).Should(Succeed())

			// check that the job has been created and has valid properties
			job := &batchv1.Job{}
			jobLookupKey := types.NamespacedName{Name: TaskExecution, Namespace: Namespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, jobLookupKey, job)).To(Succeed())
				g.Expect(job.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
				g.Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
				g.Expect(job.Spec.Template.Spec.Containers[0].Name).To(Equal("task-execution-001"))
				g.Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("busybox"))
				g.Expect(job.Spec.Template.Spec.Containers[0].Command).To(HaveLen(2))
				g.Expect(job.Spec.Template.Spec.Containers[0].Command[0]).To(Equal("echo"))
				g.Expect(job.Spec.Template.Spec.Containers[0].Command[1]).To(Equal("I am task 1 of service 1"))
				g.Expect(*job.Spec.BackoffLimit).To(Equal(int32(4)))
				g.Expect(isJobComplete(job)).To(BeTrue())
			}, timeout, interval).Should(Succeed())

			// check that the taskExecution has been marked as successful
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, taskExecutionLookupKey, taskExecution)).To(Succeed())
				g.Expect(taskExecution.Status.Conditions).To(HaveLen(1))
				g.Expect(meta.IsStatusConditionTrue(taskExecution.Status.Conditions, landscape.TaskSucceeded)).To(BeTrue())
			}, timeout, interval).Should(Succeed())
		})
	})
})

func isJobComplete(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}
