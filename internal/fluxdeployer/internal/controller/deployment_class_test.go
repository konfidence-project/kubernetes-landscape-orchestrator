package controller

import (
	"context"

	"github.com/konfidence-project/kubernetes-landscape-orchestrator/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	konfidencev1alpha1 "github.com/konfidence-project/konfidence/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DeploymentClass ownership", func() {
	It("is inactive when no matching class exists", func() {
		client := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()

		active, err := deploymentClassActive(context.Background(), client, internal.DeploymentClassHelm)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeFalse())
	})

	It("accepts a class when name and controller match", func() {
		class := &konfidencev1alpha1.DeploymentClass{
			ObjectMeta: metav1.ObjectMeta{Name: internal.DeploymentClassHelm},
			Spec: konfidencev1alpha1.DeploymentClassSpec{
				Controller: internal.ControllerName,
			},
		}
		client := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(class).Build()

		active, err := deploymentClassActive(context.Background(), client, internal.DeploymentClassHelm)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeTrue())
	})

	It("rejects a class owned by another controller", func() {
		class := &konfidencev1alpha1.DeploymentClass{
			ObjectMeta: metav1.ObjectMeta{Name: internal.DeploymentClassHelm},
			Spec: konfidencev1alpha1.DeploymentClassSpec{
				Controller: "example.com/other-controller",
			},
		}
		client := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(class).Build()

		active, err := deploymentClassActive(context.Background(), client, internal.DeploymentClassHelm)
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeFalse())
	})
})
