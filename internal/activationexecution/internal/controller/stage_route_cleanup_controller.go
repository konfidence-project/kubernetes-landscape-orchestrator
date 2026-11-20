package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const StageRouteCleanupControllerName = "kubernetes-stage-route-cleanup-controller"

// StageRouteCleanupReconciler deletes a stage's HTTPRoute from the local cluster when the Stage is deleted.
// TODO(konfidence-project/kubernetes-landscape-orchestrator#78): Delete the HTTPRoute
// from the cluster where it was created before removing the Stage finalizer.
type StageRouteCleanupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=stages,verbs=get;list;watch;update;patch

func (r *StageRouteCleanupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	stage := &konfidencev1alpha1.Stage{}
	if err := r.Get(ctx, req.NamespacedName, stage); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if stage.DeletionTimestamp.IsZero() || !controllerutil.ContainsFinalizer(stage, stageRouteFinalizer) {
		return ctrl.Result{}, nil
	}

	route := &gwapiv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: StageRouteName(stage.Name), Namespace: req.Namespace}}
	if err := client.IgnoreNotFound(r.Delete(ctx, route)); err != nil {
		return ctrl.Result{}, fmt.Errorf("delete HTTPRoute for stage %s: %w", stage.Name, err)
	}

	patch := client.MergeFrom(stage.DeepCopy())
	controllerutil.RemoveFinalizer(stage, stageRouteFinalizer)
	if err := r.Patch(ctx, stage, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove route finalizer from Stage %s: %w", stage.Name, err)
	}

	log.Info("cleaned up stage route", "stage", stage.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StageRouteCleanupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.Stage{}).
		Named("stageRouteCleanup").
		Complete(r)
}
