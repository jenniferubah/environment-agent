package monitoring_test

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/dcm-project/environment-agent/internal/openshift/container/dcm"
	"github.com/dcm-project/environment-agent/internal/openshift/container/monitoring"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("Status Monitor", func() {
	Describe("Lifecycle", func() {
		var (
			client    *fake.Clientset
			publisher *mockStatusPublisher
			logger    *slog.Logger
			cfg       monitoring.MonitorConfig
		)

		BeforeEach(func() {
			client = fake.NewClientset()
			publisher = newMockPublisher()
			logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
			cfg = monitoring.MonitorConfig{
				Namespace:    "default",
				ProviderName: "k8s-sp",
				DebounceMs:   100,
				ResyncPeriod: 1 * time.Second,
			}
		})

		It("should not process events until Start is called (TC-I049)", func() {
			monitor := monitoring.NewStatusMonitor(client, cfg, publisher, logger)

			// Create a resource before Start is called.
			replicas := int32(1)
			labels := dcm.Labels("abc-123")
			_, err := client.AppsV1().Deployments("default").Create(context.Background(), &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "pre-start", Labels: labels},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// No events should be published yet because Start hasn't been called.
			time.Sleep(200 * time.Millisecond)
			Expect(publisher.Events()).To(BeEmpty(), "no events before Start")

			// Now start and verify events arrive.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				defer GinkgoRecover()
				_ = monitor.Start(ctx)
			}()

			time.Sleep(500 * time.Millisecond)
			Expect(publisher.Events()).NotTo(BeEmpty(), "events should arrive after Start")
		})

		It("should stop watchers when context is cancelled (TC-I050)", func() {
			monitor := monitoring.NewStatusMonitor(client, cfg, publisher, logger)
			ctx, cancel := context.WithCancel(context.Background())

			started := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				close(started)
				_ = monitor.Start(ctx)
			}()
			<-started
			time.Sleep(200 * time.Millisecond)

			// Cancel the context — this should stop watchers.
			cancel()
			time.Sleep(200 * time.Millisecond)

			// Create a new resource after stop — it should NOT trigger events.
			eventsBefore := len(publisher.Events())

			replicas := int32(1)
			labels := dcm.Labels("post-stop")
			_, err := client.AppsV1().Deployments("default").Create(context.Background(), &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "post-stop", Labels: labels},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(300 * time.Millisecond)
			Expect(publisher.Events()).To(HaveLen(eventsBefore),
				"no new events after context cancellation")
		})

		It("should re-evaluate resources on cache resync (TC-I051)", func() {
			cfg.ResyncPeriod = 1 * time.Second
			monitor := monitoring.NewStatusMonitor(client, cfg, publisher, logger)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Pre-create a resource so it's in the cache.
			replicas := int32(1)
			labels := dcm.Labels("resync-test")
			_, err := client.AppsV1().Deployments("default").Create(ctx, &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "resync-deploy", Labels: labels},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			go func() {
				defer GinkgoRecover()
				_ = monitor.Start(ctx)
			}()

			// Wait long enough for at least one resync to occur (resync period + buffer).
			time.Sleep(2 * time.Second)

			events := publisher.Events()
			// On initial sync we expect at least 1 event; after resync we expect more.
			Expect(len(events)).To(BeNumerically(">=", 2),
				"resync should trigger re-evaluation")
		})

		It("should publish events for all existing resources on initial sync (TC-I052)", func() {
			// Pre-create 3 DCM-managed resources.
			for i, id := range []string{"inst-1", "inst-2", "inst-3"} {
				replicas := int32(1)
				labels := dcm.Labels(id)
				name := "deploy-" + string(rune('a'+i))
				_, err := client.AppsV1().Deployments("default").Create(context.Background(), &appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
					Spec: appsv1.DeploymentSpec{
						Replicas: &replicas,
						Selector: &metav1.LabelSelector{MatchLabels: labels},
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: labels},
							Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Image: "nginx"}}},
						},
					},
				}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
			}

			monitor := monitoring.NewStatusMonitor(client, cfg, publisher, logger)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go func() {
				defer GinkgoRecover()
				_ = monitor.Start(ctx)
			}()

			time.Sleep(1 * time.Second)

			events := publisher.Events()
			instanceIDs := make(map[string]bool)
			for _, e := range events {
				instanceIDs[e.InstanceID] = true
			}
			Expect(instanceIDs).To(HaveKey("inst-1"))
			Expect(instanceIDs).To(HaveKey("inst-2"))
			Expect(instanceIDs).To(HaveKey("inst-3"))
		})
	})
})
