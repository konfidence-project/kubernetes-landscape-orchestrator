// Package controller materialises a VectorData CR into an Immutable ConfigMap consumed by the vector-data
// service (cmd/vectordata).
//
// The ConfigMap is written exactly once per VectorData and has no owner-reference (the VectorData may live
// on a different apiserver). Its single delete path is `handleDeletion`, guarded by the finalizer
// `konfidence.cloud/vector-data-configmap-cleanup`.
package controller

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	VectorDataControllerName = "vector-data-controller"

	// VectorDataFinalizer ensures the orchestrator can clean up its ConfigMap before VectorData is finalised.
	// Required because the landscape may run on a different K8s cluster than the LCP, where ownerRef cascade does
	// not propagate. Star itself does NOT add finalizers — the two layers stay decoupled.
	VectorDataFinalizer = "konfidence.cloud/vector-data-configmap-cleanup"

	// ConfigMap data layout consumed by cmd/vectordata/vector_data_service.go. Keep both sides
	// of the constant in sync.
	ConfigMapPrefix         = "vector-data-"
	FeaturesConfigKey       = "features.json"
	AuthoredConfigKey       = "authored.json"
	DeploymentResultsPrefix = "deploymentResults."
	JSONSuffix              = ".json"

	labelManagedBy      = "konfidence.cloud/managed-by"
	labelVectorDataName = "konfidence.cloud/vector-data-name"
	labelVectorDataUID  = "konfidence.cloud/vector-data-uid"

	jsonNullLiteral = "null"
)

type VectorDataReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectordata,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectordata/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=vectordata/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;delete

func (r *VectorDataReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	vd := &konfidencev1alpha1.VectorData{}
	if err := r.Get(ctx, req.NamespacedName, vd); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !vd.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, vd, log)
	}

	if !controllerutil.ContainsFinalizer(vd, VectorDataFinalizer) {
		patch := client.MergeFrom(vd.DeepCopy())
		controllerutil.AddFinalizer(vd, VectorDataFinalizer)
		if err := r.Patch(ctx, vd, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer to VectorData %s/%s: %w", vd.Namespace, vd.Name, err)
		}
	}

	data, err := renderData(vd)
	if err != nil {
		if condErr := r.setReady(ctx, vd, metav1.ConditionFalse, "RenderFailed", err.Error()); condErr != nil {
			return ctrl.Result{}, condErr
		}
		return ctrl.Result{}, err
	}

	cmName := ConfigMapPrefix + vd.Name
	cmKey := types.NamespacedName{Namespace: vd.Namespace, Name: cmName}
	existing := &corev1.ConfigMap{}
	switch err := r.Get(ctx, cmKey, existing); {
	case err == nil:
		// ConfigMap already present. Spec is immutable upstream and the CM is Immutable=true — do not touch it.
		return ctrl.Result{}, r.setReady(ctx, vd, metav1.ConditionTrue, konfidencev1alpha1.VectorDataReasonMaterialized,
			fmt.Sprintf("ConfigMap %s already present", cmName))
	case !apierrors.IsNotFound(err):
		_ = r.setReady(ctx, vd, metav1.ConditionFalse, "ConfigMapGetFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("get ConfigMap %s: %w", cmKey, err)
	}

	immutable := true
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: vd.Namespace,
			Labels: map[string]string{
				labelManagedBy:      VectorDataControllerName,
				labelVectorDataName: vd.Name,
				labelVectorDataUID:  string(vd.UID),
			},
		},
		Immutable: &immutable,
		Data:      data,
	}
	// No owner-ref: VectorData may live on a different apiserver. Cleanup is the finalizer's job.
	if err := r.Create(ctx, cm); err != nil {
		_ = r.setReady(ctx, vd, metav1.ConditionFalse, "ConfigMapCreateFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("create ConfigMap %s: %w", cmKey, err)
	}

	if err := r.setReady(ctx, vd, metav1.ConditionTrue, konfidencev1alpha1.VectorDataReasonMaterialized,
		fmt.Sprintf("ConfigMap %s materialized", cmName)); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Eventf(vd, nil, corev1.EventTypeNormal, "VectorDataMaterialized", "VectorDataMaterialized",
		fmt.Sprintf("Materialized ConfigMap %s in namespace %s", cmName, vd.Namespace))
	log.Info("VectorData materialized", "configMap", cmKey.String())
	return ctrl.Result{}, nil
}

// renderData maps the runtime-agnostic VectorData.Spec to the data layout the vector configuration service consumes.
// Star pre-splits the OCM envelope into Spec.Features/Spec.Authored (verbatim RawExtension); we just forward them.
// DeploymentResults are keyed by artifact component; each CM entry is the JSON array of that component's results.
func renderData(vd *konfidencev1alpha1.VectorData) (map[string]string, error) {
	data := map[string]string{
		FeaturesConfigKey: rawOrNull(vd.Spec.Features),
		AuthoredConfigKey: rawOrNull(vd.Spec.Authored),
	}
	seenBasename := make(map[string]string, len(vd.Spec.DeploymentResults))
	for component, results := range vd.Spec.DeploymentResults {
		basename := componentBasename(component)
		if basename == "" {
			continue
		}
		if other, dup := seenBasename[basename]; dup {
			return nil, fmt.Errorf("components %q and %q map to the same deployment-result key %q", other, component, basename)
		}
		seenBasename[basename] = component

		if err := assertUniqueResults(component, results); err != nil {
			return nil, err
		}

		encoded, err := json.Marshal(results)
		if err != nil {
			return nil, fmt.Errorf("marshal deployment results for component %q: %w", component, err)
		}
		data[DeploymentResultsPrefix+basename+JSONSuffix] = string(encoded)
	}
	return data, nil
}

// assertUniqueResults refuses to materialise a component whose results are not unique by (Name, Type); consumers
// resolve results by that pair, so a duplicate would make the lookup ambiguous.
func assertUniqueResults(component string, results []konfidencev1alpha1.DeploymentResult) error {
	seen := make(map[string]struct{}, len(results))
	for i := range results {
		key := results[i].Name + "\x00" + results[i].Type
		if _, dup := seen[key]; dup {
			return fmt.Errorf("component %q has duplicate deployment result (name=%q, type=%q)", component, results[i].Name, results[i].Type)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func rawOrNull(r *runtime.RawExtension) string {
	if r == nil || len(r.Raw) == 0 {
		return jsonNullLiteral
	}
	return string(r.Raw)
}

// componentBasename returns the last path segment of an OCM component name; K8s ConfigMap data keys disallow `/`.
func componentBasename(component string) string {
	if idx := strings.LastIndex(component, "/"); idx >= 0 && idx < len(component)-1 {
		return component[idx+1:]
	}
	return component
}

func (r *VectorDataReconciler) handleDeletion(ctx context.Context, vd *konfidencev1alpha1.VectorData, log logr.Logger) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(vd, VectorDataFinalizer) {
		return ctrl.Result{}, nil
	}
	cmName := ConfigMapPrefix + vd.Name
	cmKey := types.NamespacedName{Namespace: vd.Namespace, Name: cmName}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: vd.Namespace}}
	if err := r.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("delete ConfigMap %s during teardown: %w", cmKey, err)
	}
	log.Info("ConfigMap removed during VectorData teardown", "configMap", cmKey.String())

	patch := client.MergeFrom(vd.DeepCopy())
	controllerutil.RemoveFinalizer(vd, VectorDataFinalizer)
	if err := r.Patch(ctx, vd, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer from VectorData %s/%s: %w", vd.Namespace, vd.Name, err)
	}
	return ctrl.Result{}, nil
}

func (r *VectorDataReconciler) setReady(ctx context.Context, vd *konfidencev1alpha1.VectorData, status metav1.ConditionStatus, reason, message string) error {
	patch := client.MergeFrom(vd.DeepCopy())
	meta.SetStatusCondition(&vd.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.VectorDataReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: vd.Generation,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, vd, patch); err != nil {
		return fmt.Errorf("patch VectorData status: %w", err)
	}
	return nil
}

func (r *VectorDataReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Reconcile on spec changes and on deletion-timestamp transitions so the finalizer path
	// runs independent of whether the API server bumps generation on the deletion write.
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.VectorData{}, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
				return e.ObjectOld.GetDeletionTimestamp() != e.ObjectNew.GetDeletionTimestamp()
			}},
		))).
		Named(VectorDataControllerName).
		Complete(r)
}
