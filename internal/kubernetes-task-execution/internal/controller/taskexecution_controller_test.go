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

	landscape "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	testutil "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/kubernetes-task-execution/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		testutil.CleanupJobs(k8sClient)
	})

	AfterEach(func() {
		testutil.CleanupTaskExecution(k8sClient, TaskExecution, Namespace)
		testutil.CleanupJobs(k8sClient)
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
			jobs := &batchv1.JobList{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.List(ctx, jobs, client.InNamespace(Namespace))).To(Succeed())
				g.Expect(jobs.Items).To(HaveLen(1))
				g.Expect(jobs.Items[0].Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
				g.Expect(jobs.Items[0].Spec.Template.Spec.Containers).To(HaveLen(1))
				g.Expect(jobs.Items[0].Spec.Template.Spec.Containers[0].Name).To(Equal("task-execution-001"))
				g.Expect(jobs.Items[0].Spec.Template.Spec.Containers[0].Image).To(Equal("busybox"))
				g.Expect(jobs.Items[0].Spec.Template.Spec.Containers[0].Command).To(HaveLen(2))
				g.Expect(jobs.Items[0].Spec.Template.Spec.Containers[0].Command[0]).To(Equal("echo"))
				g.Expect(jobs.Items[0].Spec.Template.Spec.Containers[0].Command[1]).To(Equal("I am task 1 of service 1"))
				g.Expect(*jobs.Items[0].Spec.BackoffLimit).To(Equal(int32(4)))
			}, timeout, interval).Should(Succeed())

			// set the job to completed
			job := jobs.Items[0]
			startTime := metav1.Now()
			completionTime := metav1.NewTime(startTime.Add(time.Minute))
			job.Status.StartTime = &startTime
			job.Status.CompletionTime = &completionTime
			job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{Type: batchv1.JobSuccessCriteriaMet, Status: corev1.ConditionTrue})
			job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{Type: batchv1.JobComplete, Status: corev1.ConditionTrue})
			Eventually(func(g Gomega) {
				Expect(k8sClient.Status().Update(ctx, &job)).To(Succeed())
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
