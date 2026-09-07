package internal

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInternal(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Internal Suite")
}

var _ = Describe("KnownClasses", func() {
	It("returns the known classes as a string", func() {
		Expect(KnownClasses.String()).To(Equal("[helm.konfidence.cloud kustomize.konfidence.cloud]"))
	})
})
