// Package controller reconciles DeploymentTarget resources for the kubernetes-landscape-orchestrator.
//
// The controller owns DeploymentTargets whose spec.deploymentClassName references an active DeploymentClass
// owned by this controller. Ownership is determined dynamically on each reconcile by querying the informer
// cache.
package controller

import (
	"context"
	"fmt"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// DeploymentTargetReasonUnsupportedType indicates that the DeploymentClass is not supported by this controller version.
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

	kindSecret = "Secret"
)

// Currently the user cannot specify which key should be used in a Secret/ConfigMap, so we
// have to fall back to the default keys used by FluxCD:
// https://fluxcd.io/flux/components/kustomize/kustomizations/#secret-based-authentication
var acceptedKubeconfigKeys = []string{"value", "value.yaml"}

// DeploymentTargetReconciler reconciles DeploymentTarget resources whose spec.deploymentClassName
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

	owned, err := r.ownsDeploymentTarget(ctx, dt)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !owned {
		return ctrl.Result{}, nil
	}

	log.Info("reconciling DeploymentTarget", "deploymentClassName", dt.Spec.DeploymentClassName)

	if _, known := internal.KnownClasses[dt.Spec.DeploymentClassName]; !known {
		// The DeploymentClass exists and is ours, but this version of KLO does not have a handler for it.
		return ctrl.Result{}, r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonUnsupportedType,
			fmt.Sprintf("DeploymentClass %q is not supported by this controller version; known classes: %v", dt.Spec.DeploymentClassName, internal.KnownClasses))
	}

	return ctrl.Result{}, r.validate(ctx, dt)
}

func (r *DeploymentTargetReconciler) validate(ctx context.Context, dt *konfidencev1alpha1.DeploymentTarget) error {
	if dt.Spec.Connection.Type == connectionTypeLocal {
		return r.setReady(ctx, dt, metav1.ConditionTrue, DeploymentTargetReasonAccepted,
			"local control-plane cluster connection accepted")
	}

	if dt.Spec.Connection.Type != connectionTypeKubeconfig {
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonUnsupportedConnectionType,
			fmt.Sprintf("connection type %q is not supported; expected %q or %q", dt.Spec.Connection.Type, connectionTypeKubeconfig, connectionTypeLocal))
	}

	if dt.Spec.Connection.Ref == nil || dt.Spec.Connection.Ref.Kind != kindSecret {
		kind := ""
		if dt.Spec.Connection.Ref != nil {
			kind = dt.Spec.Connection.Ref.Kind
		}
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonUnsupportedRefKind,
			fmt.Sprintf("connection.ref.kind %q is not supported for connection type %q; expected %q", kind, connectionTypeKubeconfig, kindSecret))
	}

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{Namespace: dt.Namespace, Name: dt.Spec.Connection.Ref.Name}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonSecretNotFound,
				fmt.Sprintf("secret %q not found in namespace %q", dt.Spec.Connection.Ref.Name, dt.Namespace))
		}
		return fmt.Errorf("get secret %s: %w", secretKey, err)
	}

	kubeconfigBytes, err := kubeconfigFromSecret(secret)
	if err != nil {
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonInvalidSecret, err.Error())
	}

	if _, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigBytes); err != nil {
		return r.setReady(ctx, dt, metav1.ConditionFalse, DeploymentTargetReasonInvalidKubeconfig,
			fmt.Sprintf("secret %q does not contain a valid kubeconfig: %v", dt.Spec.Connection.Ref.Name, err))
	}

	return r.setReady(ctx, dt, metav1.ConditionTrue, DeploymentTargetReasonAccepted,
		fmt.Sprintf("kubeconfig in secret %q is valid", dt.Spec.Connection.Ref.Name))
}

func kubeconfigFromSecret(secret *corev1.Secret) ([]byte, error) {
	for _, key := range acceptedKubeconfigKeys {
		if data, ok := secret.Data[key]; ok {
			return data, nil
		}
	}
	return nil, fmt.Errorf("secret %q contains no kubeconfig: expected key as one of %s", secret.Name, acceptedKubeconfigKeys)
}

func (r *DeploymentTargetReconciler) setReady(
	ctx context.Context,
	dt *konfidencev1alpha1.DeploymentTarget,
	status metav1.ConditionStatus,
	reason, message string,
) error {
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

func (r *DeploymentTargetReconciler) ownsDeploymentTarget(ctx context.Context, target *konfidencev1alpha1.DeploymentTarget) (bool, error) {
	class := &konfidencev1alpha1.DeploymentClass{}
	if err := r.Get(ctx, client.ObjectKey{Name: target.Spec.DeploymentClassName}, class); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get DeploymentClass %q: %w", target.Spec.DeploymentClassName, err)
	}
	return class.Spec.Controller == internal.ControllerName, nil
}

func (r *DeploymentTargetReconciler) deploymentTargetsForClass(ctx context.Context, deploymentClassName string) []reconcile.Request {
	targets := &konfidencev1alpha1.DeploymentTargetList{}
	if err := r.List(ctx, targets); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, 0)
	for i := range targets.Items {
		target := &targets.Items[i]
		if target.Spec.DeploymentClassName == deploymentClassName {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(target)})
		}
	}
	return requests
}

func (r *DeploymentTargetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ownedTarget := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		target, ok := obj.(*konfidencev1alpha1.DeploymentTarget)
		if !ok {
			return false
		}
		owned, err := r.ownsDeploymentTarget(context.Background(), target)
		return err == nil && owned
	})
	ownedClass := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			class, ok := e.Object.(*konfidencev1alpha1.DeploymentClass)
			return ok && class.Spec.Controller == internal.ControllerName
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			class, ok := e.ObjectNew.(*konfidencev1alpha1.DeploymentClass)
			return ok && class.Spec.Controller == internal.ControllerName
		},
	}

	deploymentClassMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			dc, ok := obj.(*konfidencev1alpha1.DeploymentClass)
			if !ok {
				return nil
			}
			return r.deploymentTargetsForClass(ctx, dc.Name)
		},
	)
	secretMapper := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			secret, ok := obj.(*corev1.Secret)
			if !ok {
				return nil
			}
			targets := &konfidencev1alpha1.DeploymentTargetList{}
			if err := r.List(ctx, targets, client.InNamespace(secret.Namespace)); err != nil {
				return nil
			}
			requests := make([]reconcile.Request, 0)
			for i := range targets.Items {
				target := &targets.Items[i]
				owned, _ := r.ownsDeploymentTarget(ctx, target)
				if owned && usesSecret(target, secret) {
					requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(target)})
				}
			}
			return requests
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&konfidencev1alpha1.DeploymentTarget{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}, ownedTarget)).
		Watches(&konfidencev1alpha1.DeploymentClass{}, deploymentClassMapper, builder.WithPredicates(ownedClass)).
		Watches(&corev1.Secret{}, secretMapper).
		Named("deployment-target-controller").
		Complete(r)
}

func usesSecret(target *konfidencev1alpha1.DeploymentTarget, secret *corev1.Secret) bool {
	return target.Spec.Connection.Ref != nil && target.Spec.Connection.Ref.Kind == kindSecret && target.Spec.Connection.Ref.Name == secret.Name
}
