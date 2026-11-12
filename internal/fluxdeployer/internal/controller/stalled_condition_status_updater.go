package controller

import (
	"context"
	"fmt"
	"slices"

	fluxmeta "github.com/fluxcd/pkg/apis/meta"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/konfidence/pkg/deployer/deploymenttarget"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

func markStalled(deployment *konfidencev1alpha1.ArtifactDeployment, cause *stallCause) {
	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.StalledCondition,
		Status:             metav1.ConditionTrue,
		Reason:             cause.reason,
		Message:            cause.message,
		ObservedGeneration: deployment.Generation,
	})
}

func markNotStalled(deployment *konfidencev1alpha1.ArtifactDeployment) {
	meta.SetStatusCondition(&deployment.Status.Conditions, metav1.Condition{
		Type:               konfidencev1alpha1.StalledCondition,
		Status:             metav1.ConditionFalse,
		Reason:             konfidencev1alpha1.StalledReasonNotStalled,
		Message:            "No blocking condition detected",
		ObservedGeneration: deployment.Generation,
	})
}

// deploymentTargetStall reports a stall while no DeploymentTarget for the artifact type is Ready.
func deploymentTargetStall(
	ctx context.Context, c client.Reader, deployment *konfidencev1alpha1.ArtifactDeployment,
) (*stallCause, error) {
	_, err := deploymenttarget.FindReady(ctx, c, deployment.Namespace, deployment.ArtifactType())
	if apierrors.IsNotFound(err) {
		return &stallCause{
			reason: konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentTargetSecretsMissing,
			message: fmt.Sprintf("no Ready DeploymentTarget for artifact type %q in namespace %q",
				deployment.ArtifactType(), deployment.Namespace),
		}, nil
	}
	return nil, err
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

// deploymentResultStallReasons are the DeploymentResultCreated=False reasons that only a change to the deployed
// manifests resolves.
var deploymentResultStallReasons = []string{
	konfidencev1alpha1.ArtifactDeploymentStalledReasonDeploymentResultNotUnique,
	artifactDeploymentStalledReasonExposePortNotFound,
	artifactDeploymentStalledReasonExposePortUnsupportedProtocol,
}

// deploymentResultStall reads the stored condition, as the results check is skipped unless ArtifactDeployed is True.
func deploymentResultStall(deployment *konfidencev1alpha1.ArtifactDeployment) *stallCause {
	created := meta.FindStatusCondition(deployment.Status.Conditions, konfidencev1alpha1.DeploymentResultCreatedCondition)
	if created == nil || created.Status != metav1.ConditionFalse || !slices.Contains(deploymentResultStallReasons, created.Reason) {
		return nil
	}
	return &stallCause{reason: created.Reason, message: created.Message}
}
