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

	landscape "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	e "github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// TaskExecutionReconciler reconciles a TaskExecution object
type TaskExecutionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
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

	// get taskExecution
	taskExecution := &landscape.TaskExecution{}
	if err := r.Get(ctx, req.NamespacedName, taskExecution); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// create or get job
	job := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Namespace: taskExecution.Namespace, Name: taskExecution.Name}, job)
	if err != nil {
		if !errors.IsNotFound(err) {
			return ctrl.Result{}, e.Wrap(err, "Unable to fetch job")
		} else {
			job, err := r.constructJob(taskExecution)
			if err != nil {
				return ctrl.Result{}, e.Wrap(err, "Unable to construct job from template")
			}

			if err = r.Create(ctx, job); err != nil {
				return ctrl.Result{}, e.Wrap(err, "Unable to create job")
			}

			log.V(1).Info("Created job", "job", job)
		}
	}

	finished, jobResult := r.isJobFinished(job)
	if !finished {
		return ctrl.Result{}, nil
	}

	switch jobResult {
	case batchv1.JobFailed:
		log.Info(fmt.Sprintf("Job %s failed", job.Name))
		if err := r.updateTaskExecutionStatus(ctx, req, metav1.Condition{Type: landscape.TaskFailed,
			Status: metav1.ConditionTrue, Reason: landscape.TaskFailed,
			Message: fmt.Sprintf("Reconciling TaskExecution %s failed", taskExecution.Name)}); err != nil {
			return ctrl.Result{}, e.Wrap(err, "Unable to update task execution status")
		}

		err := e.Errorf("task execution failed due to job failure")
		log.Error(err, "Task execution failed")
	case batchv1.JobComplete:
		log.Info(fmt.Sprintf("Job %s completed successfully", job.Name))
		if err := r.updateTaskExecutionStatus(ctx, req, metav1.Condition{Type: landscape.TaskSucceeded,
			Status: metav1.ConditionTrue, Reason: landscape.TaskSucceeded,
			Message: fmt.Sprintf("TaskExecution %s reconciled successfully", taskExecution.Name)}); err != nil {
			return ctrl.Result{}, e.Wrap(err, "Unable to update task execution status")
		}

		log.Info("TaskExecution reconciled")
	}

	return ctrl.Result{}, nil
}

func (r *TaskExecutionReconciler) updateTaskExecutionStatus(ctx context.Context, req ctrl.Request, condition metav1.Condition) error {
	taskExecution := &landscape.TaskExecution{}
	if err := r.Get(ctx, req.NamespacedName, taskExecution); err != nil {
		return e.Wrap(err, "Unable to fetch taskExecution")
	}

	meta.SetStatusCondition(&taskExecution.Status.Conditions, condition)
	if err := r.Status().Update(ctx, taskExecution); err != nil {
		return e.Wrap(err, "Failed to update taskExecution status")
	}

	return nil
}

func (r *TaskExecutionReconciler) constructJob(taskExecution *landscape.TaskExecution) (*batchv1.Job, error) {
	jobSpec := batchv1.JobSpec{}
	if err := json.Unmarshal(taskExecution.Spec.Spec.Raw, &jobSpec); err != nil {
		return nil, e.Wrap(err, "Unable to unmarshal taskExecution spec")
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskExecution.Name,
			Namespace: taskExecution.Namespace,
		},
		Spec: jobSpec,
	}

	if err := ctrl.SetControllerReference(taskExecution, job, r.Scheme); err != nil {
		return nil, e.Wrap(err, "Unable to set controller reference for job")
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

// SetupWithManager sets up the controller with the Manager.
func (r *TaskExecutionReconciler) SetupWithManager(mgr ctrl.Manager) error {
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
		For(&landscape.TaskExecution{}).WithEventFilter(taskExecutionFilter).
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
