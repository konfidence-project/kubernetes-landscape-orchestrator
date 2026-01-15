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
	"fmt"
	"reflect"

	landscape "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const TaskExecutionControllerName = "task-execution-controller"

// TaskExecutionReconciler reconciles a TaskExecution object
type TaskExecutionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=taskexecutions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=taskexecutions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=landscape.konfidence.cloud,resources=taskexecutions/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the TaskExecution object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *TaskExecutionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling taskExecution")

	taskExecution := &landscape.TaskExecution{}
	if err := r.Get(ctx, req.NamespacedName, taskExecution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalTaskExecution := taskExecution.DeepCopy()
	patch := client.MergeFrom(originalTaskExecution)
	job, err := r.createOrGetJob(ctx, taskExecution)
	if err != nil {
		return ctrl.Result{}, err
	}

	finished, jobResult := r.isJobFinished(job)
	if !finished {
		return ctrl.Result{}, nil
	}

	switch jobResult {
	case batchv1.JobFailed:
		log.Info(fmt.Sprintf("Job %s failed", job.Name))
		meta.SetStatusCondition(&taskExecution.Status.Conditions, metav1.Condition{Type: landscape.TaskFailed,
			Status: metav1.ConditionTrue, Reason: landscape.TaskFailed,
			Message: fmt.Sprintf("Reconciling TaskExecution %s failed", taskExecution.Name)})
		log.Info("Task execution failed")
	case batchv1.JobComplete:
		log.Info(fmt.Sprintf("Job %s completed successfully", job.Name))
		meta.SetStatusCondition(&taskExecution.Status.Conditions, metav1.Condition{Type: landscape.TaskSucceeded,
			Status: metav1.ConditionTrue, Reason: landscape.TaskSucceeded,
			Message: fmt.Sprintf("TaskExecution %s reconciled successfully", taskExecution.Name)})
		log.Info("TaskExecution reconciled")
	}

	if !reflect.DeepEqual(taskExecution.Status, originalTaskExecution.Status) {
		if err := r.Client.Status().Patch(ctx, taskExecution, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("unable to update taskExecution status: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *TaskExecutionReconciler) createOrGetJob(ctx context.Context, taskExecution *landscape.TaskExecution) (*batchv1.Job, error) {
	log := logf.FromContext(ctx)
	jobs := &batchv1.JobList{}
	if err := r.List(ctx, jobs, client.InNamespace(taskExecution.Namespace), client.MatchingFields{taskExecutionOwnerKey: taskExecution.Name}); err != nil {
		return nil, fmt.Errorf("unable to list jobs: %w", err)
	}

	if len(jobs.Items) > 1 {
		return nil, fmt.Errorf("multiple matching jobs found for taskExecution: %s", taskExecution.Name)
	}

	if len(jobs.Items) == 0 {
		newJob, err := r.constructJob(taskExecution)
		if err != nil {
			return nil, fmt.Errorf("unable to construct job from template: %w", err)
		}
		if err = r.Create(ctx, newJob); err != nil {
			return nil, fmt.Errorf("unable to create job: %w", err)
		}
		msg := fmt.Sprintf("Created Job %s for TaskExecution %s", newJob.Name, taskExecution.Name)
		r.Recorder.Event(taskExecution, corev1.EventTypeNormal, "JobCreated", msg)
		log.Info(msg)

		return newJob, nil
	} else {
		return &jobs.Items[0], nil
	}
}

func (r *TaskExecutionReconciler) constructJob(taskExecution *landscape.TaskExecution) (*batchv1.Job, error) {
	jobSpec := batchv1.JobSpec{}
	if err := json.Unmarshal(taskExecution.Spec.Spec.Raw, &jobSpec); err != nil {
		return nil, fmt.Errorf("unable to unmarshal taskExecution spec: %w", err)
	}

	name := fmt.Sprintf("%s-%s", taskExecution.Name, rand.String(8))
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: taskExecution.Namespace,
		},
		Spec: jobSpec,
	}

	if err := ctrl.SetControllerReference(taskExecution, job, r.Scheme); err != nil {
		return nil, fmt.Errorf("unable to set controller reference for job: %w", err)
	}

	return job, nil
}

func (r *TaskExecutionReconciler) isJobFinished(job *batchv1.Job) (bool, batchv1.JobConditionType) {
	for _, c := range job.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}

	return false, ""
}

var (
	taskExecutionOwnerKey = ".metadata.controller"
	apiGVStr              = landscape.GroupVersion.String()
)

// SetupWithManager sets up the controller with the Manager.
func (r *TaskExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &batchv1.Job{}, taskExecutionOwnerKey, func(rawObj client.Object) []string {
		// grab the job object and extract the owner
		job := rawObj.(*batchv1.Job)
		owner := metav1.GetControllerOf(job)
		if owner == nil {
			return nil
		}
		// make sure it is a taskExecution...
		if owner.APIVersion != apiGVStr || owner.Kind != landscape.TaskExecutionKind {
			return nil
		}

		// and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return fmt.Errorf("unable to create index for job owner reference: %w", err)
	}

	taskExecutionFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		switch o := obj.(type) {
		case *landscape.TaskExecution:
			return o.Spec.Type == "k8s-job"
		case *batchv1.Job:
			return r.jobOwnerRefsContainKind(o.OwnerReferences, landscape.TaskExecutionKind)
		default:
			return false
		}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&landscape.TaskExecution{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).WithEventFilter(taskExecutionFilter).
		Owns(&batchv1.Job{}).
		Named("taskExecution").
		Complete(r)
}

func (r *TaskExecutionReconciler) jobOwnerRefsContainKind(references []metav1.OwnerReference, kind string) bool {
	for _, ref := range references {
		if ref.Kind == kind {
			return true
		}
	}

	return false
}
