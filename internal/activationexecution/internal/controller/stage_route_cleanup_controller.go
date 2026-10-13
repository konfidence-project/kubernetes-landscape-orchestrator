package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const StageRouteCleanupControllerName = "kubernetes-stage-route-cleanup-controller"

// StageRouteCleanupReconciler deletes a stage's HTTPRoute when the Stage is deleted. The route may live on a different
// cluster than the Stage, so its lifecycle is bound to the Stage via a finalizer instead of owner references.
type StageRouteCleanupReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Landscape *LandscapeClientProvider
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

	if err := r.deleteStageRoute(ctx, req.Namespace, stage.Name); err != nil {
		return ctrl.Result{}, err
	}

	patch := client.MergeFrom(stage.DeepCopy())
	controllerutil.RemoveFinalizer(stage, stageRouteFinalizer)
	if err := r.Patch(ctx, stage, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove route finalizer from Stage %s: %w", stage.Name, err)
	}

	log.Info("cleaned up stage route", "stage", stage.Name)
	return ctrl.Result{}, nil
}

func (r *StageRouteCleanupReconciler) deleteStageRoute(ctx context.Context, namespace, stage string) error {
	landscapeClient, err := r.Landscape.ClientFor(ctx, namespace, "")
	if err != nil {
		return fmt.Errorf("resolve landscape client for stage %s cleanup: %w", stage, err)
	}

	route := &gwapiv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: StageRouteName(stage), Namespace: namespace}}
	if err := landscapeClient.Delete(ctx, route); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("delete HTTPRoute for stage %s: %w", stage, err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StageRouteCleanupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.Stage{}).
		Named("stageRouteCleanup").
		Complete(r)
}
