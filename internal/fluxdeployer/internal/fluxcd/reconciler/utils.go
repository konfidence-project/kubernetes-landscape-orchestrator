package reconciler

import (
	"context"
	"fmt"
	"strconv"

	fluxcd "github.com/fluxcd/pkg/apis/meta"
	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	pkgctrl "github.com/konfidence-project/konfidence/pkg/controller"
	"github.com/konfidence-project/konfidence/pkg/sanitize"
	"github.com/konfidence-project/konfidence/pkg/secret"
	"github.com/konfidence-project/konfidence/pkg/url"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/config"
	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/utils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	conditionTypeReady               = "Ready"
	maxKustomizationNameSuffixLength = 36
)

// buildKustomizationNameSuffix builds a NameSuffix of the form -<version>-<hash>, falling
// back to -<hash> when the combined length exceeds maxKustomizationNameSuffixLength.
// Both values are read from annotations attached to the deployment.
func buildKustomizationNameSuffix(deployment *konfidencev1alpha1.ArtifactDeployment) string {
	ann := deployment.GetAnnotations()
	version := ann[pkgctrl.ArtifactVersionAnnotation]
	hash := ann[pkgctrl.ArtifactHashAnnotation]

	if full := version + "-" + hash; len(full)+1 <= maxKustomizationNameSuffixLength {
		return "-" + utils.SanitizeK8sResourceName(full)
	}
	return "-" + utils.SanitizeK8sResourceName(hash)
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

	// A ConfigMap mapping is an explicit credential configuration; the domain-name
	// fallback is only a convention.
	configured := secretNameByConfigMap != ""
	secretName := secretNameByConfigMap
	if secretName == "" {
		secretName = sanitize.ResourceName(domain)
	}

	// Reference the pull secret only if it exists in the deployment namespace.
	// A missing configured secret is a configuration error; a missing fallback
	// secret means no credentials are configured, so pull anonymously.
	key := types.NamespacedName{Namespace: deployment.GetNamespace(), Name: secretName}
	if err := k8sClient.Get(ctx, key, &corev1.Secret{}); err != nil {
		if apierrors.IsNotFound(err) {
			if configured {
				return nil, fmt.Errorf("pull secret %q configured for registry %q but not found in namespace %q", secretName, domain, deployment.GetNamespace())
			}
			log.Info(fmt.Sprintf("no pull secret %q in namespace %q; pulling anonymously", secretName, deployment.GetNamespace()))
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get pull secret %q: %w", secretName, err)
	}

	return &fluxcd.LocalObjectReference{Name: secretName}, nil
}
