package kubernetes_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("K8s Store", func() {
	Describe("Delete Operations", func() {
		// TC-I037: Delete removes Deployment and associated Service
		It("removes Deployment and associated Service (TC-I037)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create Deployment and Service
			err := createFakeDeployment(client, "my-app", "abc-123")
			Expect(err).NotTo(HaveOccurred())
			err = createFakeService(client, "default", "my-app", "abc-123", corev1.ServiceTypeClusterIP, []int32{8080})
			Expect(err).NotTo(HaveOccurred())

			err = s.Delete(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment is deleted
			_, deployErr := client.AppsV1().Deployments("default").Get(context.Background(), "my-app", metav1.GetOptions{})
			Expect(deployErr).To(HaveOccurred())

			// Verify Service is deleted
			_, svcErr := client.CoreV1().Services("default").Get(context.Background(), "my-app", metav1.GetOptions{})
			Expect(svcErr).To(HaveOccurred())

			// Verify subsequent Get returns not-found
			_, getErr := s.Get(context.Background(), "abc-123")
			var notFoundErr *store.NotFoundError
			Expect(errors.As(getErr, &notFoundErr)).To(BeTrue())
		})

		// TC-I038: Delete succeeds when no Service exists
		It("succeeds when no Service exists (TC-I038)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create only Deployment, no Service
			err := createFakeDeployment(client, "my-app", "abc-123")
			Expect(err).NotTo(HaveOccurred())

			err = s.Delete(context.Background(), "abc-123")
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment is deleted
			_, deployErr := client.AppsV1().Deployments("default").Get(context.Background(), "my-app", metav1.GetOptions{})
			Expect(deployErr).To(HaveOccurred())
		})

		// TC-I103: Delete returns conflict when multiple Deployments share the same instance ID
		It("returns conflict when multiple Deployments share the same instance ID (TC-I103)", func() {
			s, client := newTestStore(defaultConfig())

			// Create two Deployments with the same instance ID but different names
			err := createFakeDeployment(client, "app-one", "dup-id")
			Expect(err).NotTo(HaveOccurred())
			err = createFakeDeployment(client, "app-two", "dup-id")
			Expect(err).NotTo(HaveOccurred())

			err = s.Delete(context.Background(), "dup-id")

			var conflictErr *store.ConflictError
			Expect(errors.As(err, &conflictErr)).To(BeTrue(), "expected ConflictError, got: %v", err)

			// Verify neither Deployment was deleted
			deployList, listErr := client.AppsV1().Deployments("default").List(context.Background(), metav1.ListOptions{})
			Expect(listErr).NotTo(HaveOccurred())
			Expect(deployList.Items).To(HaveLen(2))
		})

		// TC-I039: Delete returns not-found for non-existent container
		It("returns not-found for non-existent container (TC-I039)", func() {
			s, _ := newTestStore(defaultConfig())

			err := s.Delete(context.Background(), "xyz-999")

			var notFoundErr *store.NotFoundError
			Expect(errors.As(err, &notFoundErr)).To(BeTrue(), "expected NotFoundError, got: %v", err)
		})
	})
})
