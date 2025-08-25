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

func buildHelmRepositoryResourceName(
	deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) string {

	return utils.SanitizeK8sResourceName(fmt.Sprintf("%s-%s",
		utils.Must(utils.ParseHostnameWithPortFromURL(ocmResource.Image)),
		utils.Must(utils.GetKonfidenceLabel(&deployment.ObjectMeta, "vector-deployment-id"))))
}

func buildResourceName(
	deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) string {

	return utils.SanitizeK8sResourceName(fmt.Sprintf("%s-%s",
		ocmResource.Name, utils.Must(utils.GetKonfidenceLabel(&deployment.ObjectMeta, "vector-deployment-id"))))
}

func isInsecure(deployment *landscapev1alpha1.ArtifactDeployment) bool {
	label, err := utils.GetKonfidenceLabel(&deployment.ObjectMeta, "registry-insecure")
	isInsecure, err := strconv.ParseBool(label)
	return err == nil && isInsecure // true if insecure is true and no parsing error
}

func getSecretRef(
	deployment *landscapev1alpha1.ArtifactDeployment, ocmResource *landscapev1alpha1.OCMResource) *fluxcd.LocalObjectReference {

	label, err := utils.GetKonfidenceLabel(&deployment.ObjectMeta, "registry-skip-auth")
	skipAuth, err := strconv.ParseBool(label)

	if skipAuth && err == nil { // nil if skipAuth is true and no parsing error
		return nil
	} else {
		return &fluxcd.LocalObjectReference{
			Name: utils.SanitizeK8sResourceName(utils.Must(utils.ParseHostnameWithPortFromURL(ocmResource.Image))),
		}
	}
}
