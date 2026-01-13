/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package reconciler

import (
	"context"
	"fmt"
	"strconv"

	fluxcd "github.com/fluxcd/pkg/apis/meta"
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd/utils"
	"github.com/konfidence-project/pkg/sanitize"
	secr "github.com/konfidence-project/pkg/secret"
	"github.com/konfidence-project/pkg/url"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	conditionTypeReady   = "Ready"
	DefaultConfigMapName = "flux-deployer-configuration"
)

func buildHelmRepositoryResourceName(
	deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) string {

	return utils.SanitizeK8sResourceName(fmt.Sprintf("%s-%s", deployment.Name[:6], ocmResource.Name))
}

func buildResourceName(
	deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) string {

	return utils.SanitizeK8sResourceName(fmt.Sprintf("%s-%s",
		ocmResource.Name, deployment.Name[:6]))
}

func isInsecure(deployment *landscapev1alpha1.ArtifactDeployment) bool {
	label, err := utils.GetKonfidenceLabel(&deployment.ObjectMeta, "registry-insecure")
	if err != nil {
		return false
	}
	isInsecure, err := strconv.ParseBool(label)
	return err == nil && isInsecure // true if insecure is true and no parsing error
}

func getSecretRef(ctx context.Context, k8sClient client.Client, deployment *landscapev1alpha1.ArtifactDeployment, repositoryString string) (*fluxcd.LocalObjectReference, error) {
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
	secretNameByConfigMap, err := secr.GetSecretByConfigMap(ctx, k8sClient, DefaultConfigMapName, domain)
	if err != nil {
		return nil, err
	}

	secretName := secretNameByConfigMap
	if secretName == "" {
		// alternatively use the domain name as secret name
		secretName = sanitize.DNSSubdomainName(domain)
	}

	return &fluxcd.LocalObjectReference{Name: secretName}, nil
}
