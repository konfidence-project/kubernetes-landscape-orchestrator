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

	landscapev1alpha1 "github.com/konfidence-project/crds/api/landscape/v1alpha1"
	. "github.com/konfidence-project/landscape-flux-deployer/internal/fluxcd/reconciler/mocks"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("util functions", func() {
	var (
		clientMock *MockClient
		mockCtrl   *gomock.Controller
		ctx        context.Context
	)

	const (
		RegistryUrl               = "test.registry.com:5100/ocm/repo"
		KonfidenceSystemNamespace = "konfidence-system"
		ConfigMapName             = "flux-deployer-configuration"
		AuthConfigMapKey          = "authenticationSecretRefs"
		MappedSecretName          = "secret-123"
		HostName                  = "test.registry.com"
		SecretName                = "test-registry-com"
		LabelName                 = "konfidence.cloud/registry-skip-auth"
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		clientMock = NewMockClient(mockCtrl)
		ctx = context.Background()
	})

	AfterEach(func() {

	})

	Context("When resolving secret ref", func() {
		It("should successfully extract secret from ConfigMap", func() {
			configMap := &v1.ConfigMap{Data: map[string]string{
				AuthConfigMapKey: HostName + ": " + MappedSecretName,
			}}
			clientMock.EXPECT().Get(ctx, types.NamespacedName{
				Namespace: KonfidenceSystemNamespace,
				Name:      ConfigMapName,
			}, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ types.NamespacedName, obj interface{}, _ ...interface{}) error {
					*obj.(*v1.ConfigMap) = *configMap
					return nil
				})

			deployment := &landscapev1alpha1.ArtifactDeployment{}
			secretRef, err := getSecretRef(ctx, clientMock, deployment, RegistryUrl)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(secretRef.Name).To(gomega.Equal(MappedSecretName))
		})
	})
	It("should use domain name as secret name if config map has no matching entry", func() {
		configMap := &v1.ConfigMap{}
		clientMock.EXPECT().Get(ctx, types.NamespacedName{
			Namespace: KonfidenceSystemNamespace,
			Name:      ConfigMapName,
		}, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ types.NamespacedName, obj interface{}, _ ...interface{}) error {
				*obj.(*v1.ConfigMap) = *configMap
				return nil
			})

		deployment := &landscapev1alpha1.ArtifactDeployment{}
		secretRef, err := getSecretRef(ctx, clientMock, deployment, RegistryUrl)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(secretRef.Name).To(gomega.Equal(SecretName))
	})
	It("should return nil secretRef if auth is disabled in deployment", func() {
		deployment := &landscapev1alpha1.ArtifactDeployment{}
		deployment.SetLabels(map[string]string{LabelName: "true"})
		secretRef, err := getSecretRef(ctx, clientMock, deployment, RegistryUrl)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(secretRef).To(gomega.BeNil())
	})
})
