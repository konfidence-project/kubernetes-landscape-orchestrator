package internal

import (
	"fmt"
	"maps"
	"slices"
)

const (
	ControllerName = "konfidence.cloud/kubernetes-landscape-orchestrator"

	DeploymentClassHelm      = "helm.konfidence.cloud"
	DeploymentClassKustomize = "kustomize.konfidence.cloud"

	// DeploymentTargetReasonSecretNotFound indicates that the referenced Secret does not exist.
	DeploymentTargetReasonSecretNotFound = "SecretNotFound"
	// DeploymentTargetReasonInvalidSecret indicates that the referenced Secret has no recognized kubeconfig key.
	DeploymentTargetReasonInvalidSecret = "InvalidSecret"
)

type knownClasses map[string]struct{}

var KnownClasses = knownClasses{
	DeploymentClassHelm:      {},
	DeploymentClassKustomize: {},
}

func (c knownClasses) String() string {
	// return comma-separated map keys
	return fmt.Sprintf("%v", slices.Sorted(maps.Keys(c)))
}
