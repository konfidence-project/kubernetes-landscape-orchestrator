package reconciler

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = Describe("util functions", func() {
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

	var (
		ctx    context.Context
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		gomega.Expect(corev1.AddToScheme(scheme)).To(gomega.Succeed())
		gomega.Expect(konfidencev1alpha1.AddToScheme(scheme)).To(gomega.Succeed())
	})

	Context("When resolving secret ref", func() {
		It("should successfully extract secret from ConfigMap", func() {
			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Namespace: KonfidenceSystemNamespace, Name: ConfigMapName},
				Data:       map[string]string{AuthConfigMapKey: HostName + ": " + MappedSecretName},
			}).Build()

			deployment := &konfidencev1alpha1.ArtifactDeployment{}
			secretRef, err := getSecretRef(ctx, cl, deployment, RegistryUrl)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(secretRef.Name).To(gomega.Equal(MappedSecretName))
		})
	})
	It("should use domain name as secret name if config map has no matching entry", func() {
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: KonfidenceSystemNamespace, Name: ConfigMapName},
		}).Build()

		deployment := &konfidencev1alpha1.ArtifactDeployment{}
		secretRef, err := getSecretRef(ctx, cl, deployment, RegistryUrl)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(secretRef.Name).To(gomega.Equal(SecretName))
	})
	It("should return nil secretRef if auth is disabled in deployment", func() {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()

		deployment := &konfidencev1alpha1.ArtifactDeployment{}
		deployment.SetLabels(map[string]string{LabelName: "true"})
		secretRef, err := getSecretRef(ctx, cl, deployment, RegistryUrl)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(secretRef).To(gomega.BeNil())
	})
})
