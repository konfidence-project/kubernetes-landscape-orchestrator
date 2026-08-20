package utils

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/fluxcd/pkg/apis/meta"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	"github.com/konfidence-project/konfidence/pkg/sanitize"
	"github.com/konfidence-project/konfidence/pkg/secret"
	"github.com/konfidence-project/konfidence/pkg/url"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// SanitizeK8sResourceName makes a string DNS-1123 compatible (valid K8s resource name)
func SanitizeK8sResourceName(name string) string {
	// lowercase the name
	name = strings.ToLower(name)

	// replace any character not allowed with a dash
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	name = reg.ReplaceAllString(name, "-")

	// trim leading/trailing non-alphanumeric characters
	name = strings.TrimLeftFunc(name, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	name = strings.TrimRightFunc(name, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})

	// truncate to 63 characters max
	if len(name) > 63 {
		name = name[:63]
	}

	return name
}

func GetKonfidenceLabel(meta metav1.Object, label string) (string, error) {
	value := meta.GetLabels()[fmt.Sprintf("konfidence.cloud/%s", label)]
	if value == "" {
		return "", fmt.Errorf("label %s not found in metadata", label)
	}
	return value, nil
}

// ParseHostnameWithPortFromURL extracts the hostname (incl. port) from a URL-like string
func ParseHostnameWithPortFromURL(stringUrl string) (string, error) {
	// split the string at the first "/" to separate host:port from the rest
	parts := strings.SplitN(removeProtocol(stringUrl), "/", 2)

	if len(parts) < 1 {
		return "", fmt.Errorf("invalid URL: %s", stringUrl)
	}
	return parts[0], nil
}

func AllowInsecure(obj metav1.Object) bool {
	label, err := GetKonfidenceLabel(obj, "registry-insecure")
	if err != nil {
		return false
	}
	isInsecure, err := strconv.ParseBool(label)
	return err == nil && isInsecure // true if insecure is true and no parsing error
}

func GetSecretRef(
	ctx context.Context, k8sClient client.Client, deployment *konfidencev1alpha1.ArtifactDeployment, repositoryString string,
) (*meta.LocalObjectReference, error) {
	log := logf.FromContext(ctx)
	label, labelErr := GetKonfidenceLabel(&deployment.ObjectMeta, "registry-skip-auth")
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

	return &meta.LocalObjectReference{Name: secretName}, nil
}

func removeProtocol(stringUrl string) string {
	parts := strings.SplitN(stringUrl, "//", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return stringUrl
}

// TODO karsten: move to core/api package, method directly on konfidencev1alpha1.OCMComponent type
func SingleOCMResource(component konfidencev1alpha1.OCMComponent, resourceType string) (*konfidencev1alpha1.OCMResource, error) {
	var match *konfidencev1alpha1.OCMResource
	count := 0
	for i := range component.Resources {
		if component.Resources[i].Type == resourceType {
			count++
			match = &component.Resources[i]
		}
	}
	if count == 1 {
		return match, nil
	}

	return nil, fmt.Errorf("expected exactly one OCM resource of type %q (found %d); refusing to reconcile", resourceType, count)
}
