package reconciler

import (
	"context"
	"fmt"
	"strconv"

	fluxcd "github.com/fluxcd/pkg/apis/meta"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/konfidence/pkg/sanitize"
	"github.com/konfidence-project/konfidence/pkg/secret"
	"github.com/konfidence-project/konfidence/pkg/url"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/config"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	conditionTypeReady = "Ready"
)

func buildHelmRepositoryResourceName(
	deployment *konfidencev1alpha1.ArtifactDeployment, ocmResource *konfidencev1alpha1.OCMResource) string {

	return utils.SanitizeK8sResourceName(fmt.Sprintf("%s-%s", deployment.Name[:6], ocmResource.Name))
}

func buildResourceName(
	deployment *konfidencev1alpha1.ArtifactDeployment, ocmResource *konfidencev1alpha1.OCMResource) string {

	return utils.SanitizeK8sResourceName(fmt.Sprintf("%s-%s",
		ocmResource.Name, deployment.Name[:6]))
}

func isInsecure(deployment *konfidencev1alpha1.ArtifactDeployment) bool {
	label, err := utils.GetKonfidenceLabel(&deployment.ObjectMeta, "registry-insecure")
	if err != nil {
		return false
	}
	isInsecure, err := strconv.ParseBool(label)
	return err == nil && isInsecure // true if insecure is true and no parsing error
}

func getSecretRef(
	ctx context.Context, k8sClient client.Client, deployment *konfidencev1alpha1.ArtifactDeployment, repositoryString string,
) (*fluxcd.LocalObjectReference, error) {
	log := logf.FromContext(ctx)
	label, labelErr := utils.GetKonfidenceLabel(&deployment.ObjectMeta, "registry-skip-auth")
	skipAuth, parseErr := strconv.ParseBool(label)

	if labelErr == nil && parseErr == nil && skipAuth { // nil if skipAuth is true and no parsing error
		return nil, nil
	}

	// TODO this might not be a plain URL. Check again/possible refactor code
	// TODO when OCM version 2 has been released
	domain, err := url.ExtractHostname(repositoryString)
	if err != nil {
		return nil, fmt.Errorf("failed to extract domain from registry url: %w", err)
	}

	if domain == "" {
		log.Info(fmt.Sprintf("Could not extract domain from url %q", repositoryString))
		return nil, nil
	}

	// first try to get via default configMap
	secretNameByConfigMap, err := secret.GetSecretByConfigMap(ctx, k8sClient, config.DefaultConfigMapName, domain)
	if err != nil {
		return nil, err
	}

	secretName := secretNameByConfigMap
	if secretName == "" {
		// alternatively use the domain name as secret name
		secretName = sanitize.ResourceName(domain)
	}

	return &fluxcd.LocalObjectReference{Name: secretName}, nil
}
