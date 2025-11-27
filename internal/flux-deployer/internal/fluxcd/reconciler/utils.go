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
	"fmt"
	"strconv"

	fluxcd "github.com/fluxcd/pkg/apis/meta"
	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	"github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd/utils"
)

const conditionTypeReady = "Ready"

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

func getSecretRef(deployment *landscapev1alpha1.ArtifactDeployment, repositoryString string) *fluxcd.LocalObjectReference {
	// TODO (karsten/max # 2025-09-18) how to properly handle secrets? will be addressed with https://github.com/konfidence-project/konfidence-project/issues/259

	label, labelErr := utils.GetKonfidenceLabel(&deployment.ObjectMeta, "registry-skip-auth")
	skipAuth, parseErr := strconv.ParseBool(label)

	if labelErr == nil && parseErr == nil && skipAuth { // nil if skipAuth is true and no parsing error
		return nil
	} else {
		return &fluxcd.LocalObjectReference{
			Name: utils.SanitizeK8sResourceName(utils.Must(utils.ParseHostnameWithPortFromURL(repositoryString))),
		}
	}
}
