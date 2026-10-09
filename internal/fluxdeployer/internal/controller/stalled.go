package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deploymentTargetSecretsDeadline is how long a DeploymentTarget may wait for its kubeconfig Secret before the
// ArtifactDeployments it serves are reported stalled. Secrets are often synced in asynchronously.
const deploymentTargetSecretsDeadline = 5 * time.Minute

type stallCause struct {
	reason  string
	message string
}

type stallCheck func() (*stallCause, error)

func firstStall(checks ...stallCheck) (*stallCause, error) {
	for _, check := range checks {
		if cause, err := check(); cause != nil || err != nil {
			return cause, err
		}
	}
	return nil, nil
}

func setStalledCondition(deployment *konfidencev1alpha1.ArtifactDeployment, cause *stallCause) {
	condition := metav1.Condition{
		Type:               konfidencev1alpha1.StalledCondition,
		Status:             metav1.ConditionFalse,
		Reason:             konfidencev1alpha1.StalledReasonNotStalled,
		Message:            "No blocking condition detected",
		ObservedGeneration: deployment.Generation,
	}
	if cause != nil {
		condition.Status = metav1.ConditionTrue
		condition.Reason = cause.reason
		condition.Message = cause.message
	}
	meta.SetStatusCondition(&deployment.Status.Conditions, condition)
}

// deploymentTargetSecretsStall times the wait from the target's Ready=False lastTransitionTime, which only moves
// when Ready flips.
func deploymentTargetSecretsStall(
	ctx context.Context, c client.Reader, deployment *konfidencev1alpha1.ArtifactDeployment, now time.Time,
) (*stallCause, error) {
	targets := &konfidencev1alpha1.DeploymentTargetList{}
	if err := c.List(ctx, targets, client.InNamespace(deployment.Namespace)); err != nil {
		return nil, fmt.Errorf("list DeploymentTargets in namespace %q: %w", deployment.Namespace, err)
	}

	var waiting *konfidencev1alpha1.DeploymentTarget
	var waitingReady *metav1.Condition
	for i := range targets.Items {
		target := &targets.Items[i]
		if target.Spec.DeploymentClassName != deployment.Spec.Manifest.Type {
			continue
		}
		ready := meta.FindStatusCondition(target.Status.Conditions, konfidencev1alpha1.DeploymentTargetReadyCondition)
		if ready == nil || ready.ObservedGeneration != target.Generation {
			continue
		}
		if ready.Status == metav1.ConditionTrue {
			return nil, nil
		}
		if ready.Reason != internal.DeploymentTargetReasonSecretNotFound && ready.Reason != internal.DeploymentTargetReasonInvalidSecret {
			continue
		}
		if now.Sub(ready.LastTransitionTime.Time) < deploymentTargetSecretsDeadline {
			continue
		}
		if waiting == nil || target.Name < waiting.Name {
			waiting, waitingReady = target, ready
		}
	}
	if waiting == nil {
		return nil, nil
	}

	return &stallCause{
		reason: konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentTargetSecretsMissing,
		message: fmt.Sprintf("DeploymentTarget %s has had no usable kubeconfig Secret for more than %s: %s",
			waiting.Name, deploymentTargetSecretsDeadline, waitingReady.Message),
	}, nil
}

// resourceStall reports a stall when the artifact's deployable OCM resource is missing or cannot be mapped. The
// ArtifactDeployment spec is immutable, so retrying cannot fix it.
func resourceStall(resourceErr error) *stallCause {
	if resourceErr == nil {
		return nil
	}
	return &stallCause{
		reason:  konfidencev1alpha1.ArtifactDeploymentStalledReasonResourceInvalid,
		message: resourceErr.Error(),
	}
}

type fluxObject interface {
	client.Object
	GetConditions() []metav1.Condition
}

// fluxStall mirrors the Stalled condition Flux sets on one of its objects. Flux only writes Stalled=True or removes
// it, and a Stalled condition from an older generation of the object no longer applies.
func fluxStall(ctx context.Context, c client.Reader, kind string, key client.ObjectKey, obj fluxObject) (*stallCause, error) {
	if err := c.Get(ctx, key, obj); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	stalled := meta.FindStatusCondition(obj.GetConditions(), fluxmeta.StalledCondition)
	if stalled == nil || stalled.Status != metav1.ConditionTrue || stalled.ObservedGeneration != obj.GetGeneration() {
		return nil, nil
	}
	return &stallCause{
		reason:  konfidencev1alpha1.StalledReasonStalled,
		message: fmt.Sprintf("%s %s is stalled: %s", kind, key.Name, stalled.Message),
	}, nil
}

func deploymentResultStall(resultsErr error) *stallCause {
	if !errors.Is(resultsErr, errDeploymentResultNotUnique) {
		return nil
	}
	return &stallCause{
		reason:  konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentResultNotUnique,
		message: resultsErr.Error(),
	}
}
