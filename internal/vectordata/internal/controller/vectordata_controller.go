// Package controller implements the Kubernetes-runtime reconciler for the VectorData CRD. See the parent package
// doc-comment in ../setup.go for the architectural context and the ConfigMap data layout.
package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	star "github.com/konfidence-project/konfidence/api/star/v1alpha1"
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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// VectorDataControllerName is the controller name registered with the manager. Reused as the recorder source.
	VectorDataControllerName = "vector-data-controller"

	// VectorDataFinalizer drives explicit cleanup of the materialised ConfigMap on VectorData deletion (in addition
	// to the owner-reference cascade Kubernetes performs in the background).
	VectorDataFinalizer = "konfidence.cloud/vector-data-configmap-cleanup"

	// ConfigMapPrefix is the fixed prefix expected by the in-process vector configuration service
	// (see cmd/vectorconfiguration/vector_configuration_service.go: `ConfigMapPrefix`). Keeping both sides in sync
	// is a hard requirement; any change here must be made simultaneously on both sides.
	ConfigMapPrefix = "vector-data-"

	// Data-key layout consumed by the vector configuration service. Mirrors the same constants in
	// cmd/vectorconfiguration/vector_configuration_service.go.
	FeaturesConfigKey       = "features.json"
	AuthoredConfigKey       = "authored.json"
	DeploymentResultsPrefix = "deploymentResults."
	JSONSuffix              = ".json"

	// Top-level keys of the OCM-authored configuration envelope produced by the galaxy assembly side
	// (api/galaxy/v1alpha1/vector_config_types.go: `Features`, `Authored`).
	envelopeFeaturesKey = "features"
	envelopeAuthoredKey = "authored"

	// jsonNullLiteral is the JSON literal used for missing-but-present fields in the rendered ConfigMap, so the data
	// layout is shape-stable for consumers regardless of which subsets the vector author declared.
	jsonNullLiteral = "null"

	// labelManagedBy / labelVectorDataName / labelVectorDataUID record provenance on the materialised ConfigMap so
	// operators (and the in-cluster vector configuration service informer) can correlate it with the source
	// VectorData object.
	labelManagedBy      = "konfidence.cloud/managed-by"
	labelVectorDataName = "konfidence.cloud/vector-data-name"
	labelVectorDataUID  = "konfidence.cloud/vector-data-uid"
)

// VectorDataReconciler reconciles a VectorData object by materialising it as a Kubernetes ConfigMap.
type VectorDataReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=vectordata,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=vectordata/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=star.konfidence.cloud,resources=vectordata/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;delete

// Reconcile materialises a VectorData object as an immutable ConfigMap in the same namespace, then reports back by
// flipping VectorData.Status.Ready=True. On VectorData deletion the finalizer ensures the ConfigMap is removed
// explicitly before the API server completes finalisation.
func (r *VectorDataReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	vectorData := &star.VectorData{}
	if err := r.Get(ctx, req.NamespacedName, vectorData); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !vectorData.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, vectorData, log)
	}

	// Add the finalizer on first reconcile so deletion cleanup is guaranteed to run.
	if !controllerutil.ContainsFinalizer(vectorData, VectorDataFinalizer) {
		patch := client.MergeFrom(vectorData.DeepCopy())
		controllerutil.AddFinalizer(vectorData, VectorDataFinalizer)
		if err := r.Patch(ctx, vectorData, patch); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer to VectorData %s/%s: %w", vectorData.Namespace, vectorData.Name, err)
		}
	}

	data, err := renderConfigMapData(vectorData)
	if err != nil {
		if condErr := r.setReadyCondition(ctx, vectorData, metav1.ConditionFalse, "PayloadBuildFailed", err.Error()); condErr != nil {
			return ctrl.Result{}, condErr
		}
		return ctrl.Result{}, err
	}

	cmName := ConfigMapPrefix + vectorData.Name
	cmKey := types.NamespacedName{Namespace: vectorData.Namespace, Name: cmName}

	existing := &corev1.ConfigMap{}
	switch err := r.Get(ctx, cmKey, existing); {
	case err == nil:
		// VectorData.Spec is immutable upstream, so the ConfigMap we wrote earlier is the source of truth. The CM
		// itself is also Immutable=true. Just flip Ready=True idempotently.
		if condErr := r.setReadyCondition(ctx, vectorData, metav1.ConditionTrue, star.VectorDataReasonMaterialized,
			fmt.Sprintf("ConfigMap %s already present", cmName)); condErr != nil {
			return ctrl.Result{}, condErr
		}
		return ctrl.Result{}, nil
	case !apierrors.IsNotFound(err):
		_ = r.setReadyCondition(ctx, vectorData, metav1.ConditionFalse, "ConfigMapGetFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to get ConfigMap %s: %w", cmKey, err)
	}

	immutable := true
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: vectorData.Namespace,
			Labels: map[string]string{
				labelManagedBy:      VectorDataControllerName,
				labelVectorDataName: vectorData.Name,
				labelVectorDataUID:  string(vectorData.UID),
			},
		},
		Immutable: &immutable,
		Data:      data,
	}

	if err := controllerutil.SetControllerReference(vectorData, cm, r.Scheme); err != nil {
		_ = r.setReadyCondition(ctx, vectorData, metav1.ConditionFalse, "OwnerRefFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to set controller reference on ConfigMap %s: %w", cmKey, err)
	}

	if err := r.Create(ctx, cm); err != nil {
		_ = r.setReadyCondition(ctx, vectorData, metav1.ConditionFalse, "ConfigMapCreateFailed", err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to create ConfigMap %s: %w", cmKey, err)
	}

	if condErr := r.setReadyCondition(ctx, vectorData, metav1.ConditionTrue, star.VectorDataReasonMaterialized,
		fmt.Sprintf("ConfigMap %s materialized", cmName)); condErr != nil {
		return ctrl.Result{}, condErr
	}
	r.Recorder.Eventf(vectorData, nil, corev1.EventTypeNormal,
		"VectorDataMaterialized", "VectorDataMaterialized",
		fmt.Sprintf("Materialized ConfigMap %s in namespace %s", cmName, vectorData.Namespace))
	log.Info("VectorData materialized", "configMap", cmKey.String())
	return ctrl.Result{}, nil
}

// renderConfigMapData converts the runtime-agnostic VectorData.Spec into the data layout consumed by the
// in-process vector configuration service (features.json, authored.json, deploymentResults.<basename>.json).
//
// Spec.Config is expected to be a JSON object with optional top-level "features" and "authored" keys (matching the
// envelope produced by the galaxy assembly side, api/galaxy/v1alpha1/vector_config_types.go). Each subset is
// extracted and serialised as its own ConfigMap key; missing subsets are materialised as JSON null so the layout is
// shape-stable for consumers.
//
// Spec.DeploymentResults is fanned out: one key per artifact, named
// `deploymentResults.<componentBasename>.json`, where the basename is the last `/`-segment of the component name
// (Kubernetes ConfigMap data keys may not contain `/`). The value is the DeploymentResult.Spec JSON, forwarded
// verbatim — consumers (e.g. the wire-protocol service) treat it as opaque payload.
func renderConfigMapData(vd *star.VectorData) (map[string]string, error) {
	features, authored, err := splitConfigEnvelope(vd.Spec.Config)
	if err != nil {
		return nil, err
	}

	data := map[string]string{
		FeaturesConfigKey: features,
		AuthoredConfigKey: authored,
	}

	// Fan out the aggregated DeploymentResults into one key per artifact. We key on the last `/`-segment of the
	// component name; this matches what the vector configuration service tests assume (e.g. "orders-db" for a
	// component named "<repo>/orders-db") and keeps the data-key charset DNS-1123 compatible.
	for compoundKey, result := range vd.Spec.DeploymentResults {
		basename := componentBasename(compoundKey)
		if basename == "" {
			continue
		}
		// The wire-protocol service serves the DeploymentResult.Spec JSON verbatim. Forward only that, not the
		// surrounding {name,type,spec} envelope, so consumers see what they would have seen had they written the
		// payload themselves.
		specBytes := result.Spec.Raw
		if len(specBytes) == 0 {
			specBytes = []byte(jsonNullLiteral)
		}
		if !json.Valid(specBytes) {
			return nil, fmt.Errorf("deployment result %q has non-JSON Spec", compoundKey)
		}
		data[DeploymentResultsPrefix+basename+JSONSuffix] = string(specBytes)
	}
	return data, nil
}

// splitConfigEnvelope extracts the "features" and "authored" sub-objects of the OCM envelope. When the input is empty
// (no authored config on the vector at all), both outputs are the JSON literal "null" — the same shape the consumer
// would see when an envelope was present but did not declare the respective sub-object.
//
// We intentionally use a partial decode (RawMessage map) rather than a typed struct so unknown sibling fields on the
// envelope are tolerated and the controller stays decoupled from the galaxy-side struct evolution.
func splitConfigEnvelope(envelope []byte) (string, string, error) {
	if len(envelope) == 0 {
		return jsonNullLiteral, jsonNullLiteral, nil
	}
	var asMap map[string]json.RawMessage
	if err := json.Unmarshal(envelope, &asMap); err != nil {
		return "", "", fmt.Errorf("VectorData.Spec.Config is not a JSON object: %w", err)
	}
	features := jsonNullLiteral
	if raw, ok := asMap[envelopeFeaturesKey]; ok && len(raw) > 0 {
		features = string(raw)
	}
	authored := jsonNullLiteral
	if raw, ok := asMap[envelopeAuthoredKey]; ok && len(raw) > 0 {
		authored = string(raw)
	}
	return features, authored, nil
}

// componentBasename returns the last `/`-segment of a compound deployment-result key (typically `<component>/<result>`
// emitted by the Star vector-deployment-controller). The deployment result map is keyed `componentName/resultName`,
// so we take the segment immediately before the final `/`; if the format is different we fall back to the whole key.
func componentBasename(compoundKey string) string {
	// The Star side keys results "<componentName>/<resultName>". Split that first.
	slash := strings.LastIndex(compoundKey, "/")
	componentName := compoundKey
	if slash >= 0 {
		componentName = compoundKey[:slash]
	}
	// componentName itself may be a slash-rich OCM component identifier (e.g. github.com/org/repo/svc). Take its
	// basename for the ConfigMap data key.
	componentName = strings.TrimSpace(componentName)
	if componentName == "" {
		return ""
	}
	if idx := strings.LastIndex(componentName, "/"); idx >= 0 && idx < len(componentName)-1 {
		return componentName[idx+1:]
	}
	return componentName
}

// handleDeletion deletes the materialised ConfigMap explicitly and clears the finalizer.
func (r *VectorDataReconciler) handleDeletion(ctx context.Context, vectorData *star.VectorData, log logr.Logger) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(vectorData, VectorDataFinalizer) {
		return ctrl.Result{}, nil
	}
	cmName := ConfigMapPrefix + vectorData.Name
	cmKey := types.NamespacedName{Namespace: vectorData.Namespace, Name: cmName}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: vectorData.Namespace}}
	if err := r.Delete(ctx, cm); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, fmt.Errorf("failed to delete ConfigMap %s during VectorData teardown: %w", cmKey, err)
	}
	log.Info("ConfigMap removed during VectorData teardown", "configMap", cmKey.String())

	patch := client.MergeFrom(vectorData.DeepCopy())
	controllerutil.RemoveFinalizer(vectorData, VectorDataFinalizer)
	if err := r.Patch(ctx, vectorData, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from VectorData %s/%s: %w", vectorData.Namespace, vectorData.Name, err)
	}
	return ctrl.Result{}, nil
}

// setReadyCondition writes the VectorData Ready condition idempotently with a status subresource patch.
func (r *VectorDataReconciler) setReadyCondition(ctx context.Context, vd *star.VectorData, status metav1.ConditionStatus, reason, message string) error {
	patch := client.MergeFrom(vd.DeepCopy())
	meta.SetStatusCondition(&vd.Status.Conditions, metav1.Condition{
		Type:               star.VectorDataReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: vd.Generation,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, vd, patch); err != nil {
		return fmt.Errorf("failed to patch VectorData status: %w", err)
	}
	return nil
}

// SetupWithManager wires the reconciler into the manager.
func (r *VectorDataReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&star.VectorData{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.ConfigMap{}).
		Named(VectorDataControllerName).
		Complete(r)
}
