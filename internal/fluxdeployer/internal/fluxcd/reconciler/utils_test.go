package reconciler

import (
	"context"

	landscapev1alpha1 "github.com/konfidence-project/konfidence/api/star/v1alpha1"
	. "github.com/konfidence-project/kubernetes-landscape-orchestrator/internal/fluxdeployer/internal/fluxcd/reconciler/mocks"
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
