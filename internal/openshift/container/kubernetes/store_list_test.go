package kubernetes_test

import (
	"context"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/dcm-project/environment-agent/internal/openshift/container/store"
)

var _ = Describe("K8s Store", func() {
	Describe("List Operations", func() {
		// TC-I034: List supports pagination over Deployments
		It("supports pagination over Deployments (TC-I034)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create 5 Deployments
			for i := 0; i < 5; i++ {
				name := fmt.Sprintf("app-%d", i)
				id := fmt.Sprintf("id-%d", i)
				err := createFakeDeployment(client, name, id)
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := s.List(context.Background(), 2, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Containers).NotTo(BeNil())
			Expect(*result.Containers).To(HaveLen(2))
			Expect(result.NextPageToken).NotTo(BeNil())
			Expect(*result.NextPageToken).NotTo(BeEmpty())
		})

		// TC-I035: List with page_token returns subsequent page
		It("returns subsequent page with page_token (TC-I035)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create 5 Deployments
			for i := 0; i < 5; i++ {
				name := fmt.Sprintf("app-%d", i)
				id := fmt.Sprintf("id-%d", i)
				err := createFakeDeployment(client, name, id)
				Expect(err).NotTo(HaveOccurred())
			}

			// Get first page
			firstPage, err := s.List(context.Background(), 2, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(firstPage.NextPageToken).NotTo(BeNil())

			// Get second page
			secondPage, err := s.List(context.Background(), 2, *firstPage.NextPageToken)
			Expect(err).NotTo(HaveOccurred())
			Expect(secondPage).NotTo(BeNil())
			Expect(secondPage.Containers).NotTo(BeNil())

			// Verify no overlap
			firstIDs := make(map[string]bool)
			for _, c := range *firstPage.Containers {
				firstIDs[*c.Id] = true
			}
			for _, c := range *secondPage.Containers {
				Expect(firstIDs).NotTo(HaveKey(*c.Id), "second page should not overlap with first")
			}
		})

		// TC-I036: List defaults to page size of 50
		It("defaults to page size of 50 (TC-I036)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create 75 Deployments
			for i := 0; i < 75; i++ {
				name := fmt.Sprintf("app-%03d", i)
				id := fmt.Sprintf("id-%03d", i)
				err := createFakeDeployment(client, name, id)
				Expect(err).NotTo(HaveOccurred())
			}

			result, err := s.List(context.Background(), 0, "") // 0 means default
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Containers).NotTo(BeNil())
			Expect(len(*result.Containers)).To(BeNumerically("<=", 50))
			Expect(result.NextPageToken).NotTo(BeNil())
			Expect(*result.NextPageToken).NotTo(BeEmpty())
		})

		// TC-I078: List returns error for invalid page_token
		It("returns error for invalid page_token (TC-I078)", func() {
			s, client := newTestStore(defaultConfig())

			// Pre-create at least one Deployment so the store has data
			err := createFakeDeployment(client, "my-app", "id-001")
			Expect(err).NotTo(HaveOccurred())

			_, err = s.List(context.Background(), 10, "not-a-valid-token")

			var invalidErr *store.InvalidArgumentError
			Expect(errors.As(err, &invalidErr)).To(BeTrue(), "expected InvalidArgumentError, got: %v", err)
		})

		// TC-I089: List returns empty result when no Deployments exist
		It("returns empty result when no Deployments exist (TC-I089)", func() {
			s, _ := newTestStore(defaultConfig())

			result, err := s.List(context.Background(), 10, "")
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Containers).NotTo(BeNil())
			Expect(*result.Containers).To(BeEmpty())
			Expect(result.NextPageToken).To(BeNil())
		})

		// TC-I086: List returns error for negative page_token offset
		It("returns error for negative page_token offset (TC-I086)", func() {
			s, client := newTestStore(defaultConfig())

			err := createFakeDeployment(client, "my-app", "id-001")
			Expect(err).NotTo(HaveOccurred())

			// Encode "-5" as base64 to craft a negative offset token
			negativeToken := "LTU=" // base64("-5")
			_, err = s.List(context.Background(), 10, negativeToken)

			var invalidErr *store.InvalidArgumentError
			Expect(errors.As(err, &invalidErr)).To(BeTrue(), "expected InvalidArgumentError, got: %v", err)
		})
	})
})
