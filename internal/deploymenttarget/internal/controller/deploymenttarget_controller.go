// Package controller reconciles DeploymentTarget resources for the kubernetes-landscape-orchestrator.
//
// The controller owns DeploymentTargets whose spec.type is covered by an active
// DeploymentClass with spec.controller == deploymentclass.ControllerName.
// Ownership is determined dynamically on each reconcile by querying the informer
// cache.
package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/pkg/deploymentclass"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	controllerName = "konfidence.cloud/kubernetes-landscape-orchestrator"

	// DeploymentTargetReasonDeploymentClassNotFound indicates that the previously owning DeploymentClass no longer exists.
	// It is only set when there previously was a DeploymentClass pointing to this controller and that got deleted.
	DeploymentTargetReasonDeploymentClassNotFound = "DeploymentClassNotFound"
	// DeploymentTargetReasonUnsupportedType indicates that the DeploymentClass type is not supported by this controller version.
	DeploymentTargetReasonUnsupportedType = "UnsupportedType"
	// DeploymentTargetReasonUnsupportedConnectionType indicates that the connection type is not supported.
	DeploymentTargetReasonUnsupportedConnectionType = "UnsupportedConnectionType"
	// DeploymentTargetReasonUnsupportedRefKind indicates that the connection reference kind is not supported.
	DeploymentTargetReasonUnsupportedRefKind = "UnsupportedRefKind"
	// DeploymentTargetReasonSecretNotFound indicates that the referenced Secret does not exist.
	DeploymentTargetReasonSecretNotFound = "SecretNotFound"
	// DeploymentTargetReasonInvalidSecret indicates that the referenced Secret has no recognized kubeconfig key.
	DeploymentTargetReasonInvalidSecret = "InvalidSecret"
	// DeploymentTargetReasonInvalidKubeconfig indicates that the referenced Secret contains an invalid kubeconfig.
	DeploymentTargetReasonInvalidKubeconfig = "InvalidKubeconfig"
	// DeploymentTargetReasonAccepted indicates that all validation checks passed and the DeploymentTarget is ready for use.
	DeploymentTargetReasonAccepted = "Accepted"

	connectionTypeKubeconfig = "kubeconfig"
	connectionTypeLocal      = "local"
)

// DeploymentTargetReconciler reconciles DeploymentTarget resources whose spec.type
// is covered by an active DeploymentClass owned by this controller.
type DeploymentTargetReconciler struct {
	client.Client
}

// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymenttargets,verbs=get;list;watch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymenttargets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konfidence.cloud,resources=deploymentclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *DeploymentTargetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	dt := &konfidencev1alpha1.DeploymentTarget{}
	if err := r.Get(ctx, req.NamespacedName, dt); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Determine which types are currently covered by an active DeploymentClass we own.
	activeTypes, err := deploymentclass.ActiveTypes(ctx, r.Client, controllerName)
	if err != nil {
		return ctrl.Result{}, err
	}

	_, isActive := activeTypes[dt.Spec.Type]

	if !isActive {
		// We do not own this DeploymentTarget right now. However, if it already carries
		// a Ready condition from a previous reconcile (i.e. we previously owned it and
		// the DeploymentClass was since deleted), we must clear it to avoid stale state.
		if existingCond := meta.FindStatusCondition(dt.Status.Conditions, konfidencev1alpha1.DeploymentTargetReadyCondition); existingCond != nil {
			return ctrl.Result{}, r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonDeploymentClassNotFound,
				fmt.Sprintf("DeploymentClass for type %q is no longer registered by controller %q", dt.Spec.Type, controllerName))
		}
		// No prior condition — never ours. Skip silently.
		return ctrl.Result{}, nil
	}

	log.Info("reconciling DeploymentTarget", "type", dt.Spec.Type)

	// Guard against forward-compatibility: the DeploymentClass exists and is ours,
	// but this binary version does not have a handler for the type.
	if _, known := deploymentclass.KnownTypes[dt.Spec.Type]; !known {
		return ctrl.Result{}, r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonUnsupportedType,
			fmt.Sprintf("type %q is not supported by this controller version; known types: %v", dt.Spec.Type, deploymentclass.KnownTypes))
	}

	return ctrl.Result{}, r.validate(ctx, dt)
}

// validate checks the connection configuration and updates the Ready condition.
func (r *DeploymentTargetReconciler) validate(ctx context.Context, dt *konfidencev1alpha1.DeploymentTarget) error {
	if dt.Spec.Connection.Type == connectionTypeLocal {
		return r.setReady(ctx, dt, metav1.ConditionTrue, DeploymentTargetReasonAccepted,
			"local control-plane cluster connection accepted")
	}

	if dt.Spec.Connection.Type != connectionTypeKubeconfig {
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonUnsupportedConnectionType,
			fmt.Sprintf("connection type %q is not supported; expected %q or %q", dt.Spec.Connection.Type, connectionTypeKubeconfig, connectionTypeLocal))
	}

	if dt.Spec.Connection.Ref == nil || dt.Spec.Connection.Ref.Kind != "Secret" {
		kind := ""
		if dt.Spec.Connection.Ref != nil {
			kind = dt.Spec.Connection.Ref.Kind
		}
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonUnsupportedRefKind,
			fmt.Sprintf("connection.ref.kind %q is not supported for connection type %q; expected \"Secret\"", kind, connectionTypeKubeconfig))
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Namespace: dt.Namespace, Name: dt.Spec.Connection.Ref.Name}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonSecretNotFound,
				fmt.Sprintf("Secret %q not found in namespace %q", dt.Spec.Connection.Ref.Name, dt.Namespace))
		}
		return fmt.Errorf("get Secret %s: %w", secretKey, err)
	}

	kubeconfigBytes, err := kubeconfigFromSecret(secret)
	if err != nil {
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonInvalidSecret, err.Error())
	}

	if _, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes); err != nil {
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonInvalidKubeconfig,
			fmt.Sprintf("Secret %q does not contain a valid kubeconfig: %v", dt.Spec.Connection.Ref.Name, err))
	}

	return r.setReady(ctx, dt, metav1.ConditionTrue, DeploymentTargetReasonAccepted,
		fmt.Sprintf("kubeconfig in Secret %q is valid", dt.Spec.Connection.Ref.Name))
}

// kubeconfigFromSecret extracts kubeconfig bytes from a Secret.
// It tries the conventional "value" key first, then falls back to "value.yaml".
func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	for _, key := range []string{"value", "value.yaml"} {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("Secret %q contains no kubeconfig: expected key \"value\" or \"value.yaml\"", secret.Name)
}

func (r *DeploymentTargetReconciler) setReady(ctx context.Context, dt *konfidencev1alpha1.DeploymentTarget, status metav1.ConditionStatus, reason, message string) error {
	patch := client.MergeFrom(dt.DeepCopy())
	meta.SetStatusCondition(&dt.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.DeploymentTargetReadyCondition,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dt.Generation,
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, dt, patch); err != nil {
		return fmt.Errorf("patch DeploymentTarget status: %w", err)
	}
	return nil
}

func (r *DeploymentTargetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// deploymentClassMapper re-enqueues all DeploymentTargets whose spec.type matches
	// a DeploymentClass that was created, updated or deleted. This ensures stale
	// Ready=True conditions are cleared when a DeploymentClass disappears.
	deploymentClassMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			dc, ok := obj.(*konfidencev1alpha1.DeploymentClass)
			if !ok || dc.Spec.Controller != controllerName {
				return nil
			}
			return deploymentclass.DeploymentTargetsForType(ctx, r.Client, dc.Spec.Type)
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.DeploymentTarget{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&konfidencev1alpha1.DeploymentClass{}, deploymentClassMapper).
		Named("deployment-target-controller").
		Complete(r)
}
