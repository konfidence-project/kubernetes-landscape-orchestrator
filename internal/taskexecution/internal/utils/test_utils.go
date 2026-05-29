//nolint:staticcheck // ST1001: allow dot-import for test utils using Gomega
package utils

import (
	"context"

	landscape "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateTaskExecution(
	ctx context.Context, k8sClient client.Client,
	name string, namespace string, specName string, specType string, dependsOn []string, jobSpec string,
) {
	taskExecution := &landscape.TaskExecution{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "star.konfidence.cloud/v1alpha1",
			Kind:       "TaskExecution",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: landscape.TaskExecutionSpec{
			Name:      specName,
			Type:      specType,
			DependsOn: dependsOn,
			Spec: runtime.RawExtension{
				Raw: []byte(jobSpec),
			},
		},
	}

	Expect(k8sClient.Create(ctx, taskExecution)).To(Succeed())
}

func GetTaskExecution(ctx context.Context, k8sClient client.Client, name string, namespace string, opt bool) *landscape.TaskExecution {
	taskExecution := &landscape.TaskExecution{}
	taskExecutionLookupKey := types.NamespacedName{Name: name, Namespace: namespace}
	err := k8sClient.Get(ctx, taskExecutionLookupKey, taskExecution)

	if opt && err != nil && errors.IsNotFound(err) {
		return nil
	}

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch taskExecution: %s", name)
	return taskExecution
}

func DeleteTaskExecution(ctx context.Context, k8sClient client.Client, taskExecution *landscape.TaskExecution) {
	err := k8sClient.Delete(ctx, taskExecution)
	Expect(err).ToNot(HaveOccurred(), "Failed to delete taskExecution: %s", taskExecution.Name)
}

func CleanupTaskExecution(k8sClient client.Client, taskExecutionName string, namespace string) {
	ctx := context.Background()
	taskExecution := GetTaskExecution(ctx, k8sClient, taskExecutionName, namespace, true)

	if taskExecution != nil {
		DeleteTaskExecution(ctx, k8sClient, taskExecution)
	}
}

func DeleteJob(ctx context.Context, k8sClient client.Client, job *batchv1.Job) {
	err := k8sClient.Delete(ctx, job)
	Expect(err).ToNot(HaveOccurred(), "Failed to delete job: %s", job.Name)
}

func GetJobs(ctx context.Context, k8sClient client.Client) *batchv1.JobList {
	jobs := &batchv1.JobList{}
	err := k8sClient.List(ctx, jobs)

	Expect(err).ToNot(HaveOccurred(), "Failed to fetch jobs")
	return jobs
}
func CleanupJobs(k8sClient client.Client) {
	ctx := context.Background()
	jobs := GetJobs(ctx, k8sClient)

	for _, job := range jobs.Items {
		DeleteJob(ctx, k8sClient, &job)
	}
}
