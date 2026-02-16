package client

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1 "github.com/operasoftware/cnpg-plugin-pgbackrest/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var scheme = buildScheme()

func buildScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)

	return scheme
}

var _ = Describe("ExtendedClient Get", func() {
	var (
		extendedClient *ExtendedClient
		secretInClient *corev1.Secret
		archive        *v1.Archive
	)

	BeforeEach(func() {
		secretInClient = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-secret",
			},
		}
		archive = &v1.Archive{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-object-store",
			},
			Spec: v1.ArchiveSpec{},
		}

		baseClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(secretInClient, archive).Build()
		extendedClient = NewExtendedClient(baseClient).(*ExtendedClient)
	})

	It("returns secret from cache if not expired", func(ctx SpecContext) {
		secretNotInClient := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-secret-not-in-client",
			},
		}

		// manually add the secret to the cache, this is not present in the fake client so we are sure it is from the
		// cache
		extendedClient.cachedObjects = []cachedEntry{
			{
				entry:         secretNotInClient,
				fetchUnixTime: time.Now().Unix(),
				ttl:           time.Duration(DefaultTTLSeconds) * time.Second,
			},
		}

		err := extendedClient.Get(ctx, client.ObjectKeyFromObject(secretNotInClient), secretInClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(secretNotInClient).To(Equal(extendedClient.cachedObjects[0].entry))
	})

	It("fetches secret from base client if cache is expired", func(ctx SpecContext) {
		extendedClient.cachedObjects = []cachedEntry{
			{
				entry:         secretInClient.DeepCopy(),
				fetchUnixTime: time.Now().Add(-2 * time.Minute).Unix(),
				ttl:           time.Duration(DefaultTTLSeconds) * time.Second,
			},
		}

		err := extendedClient.Get(ctx, client.ObjectKeyFromObject(secretInClient), secretInClient)
		Expect(err).NotTo(HaveOccurred())
	})

	It("fetches secret from base client if not in cache", func(ctx SpecContext) {
		err := extendedClient.Get(ctx, client.ObjectKeyFromObject(secretInClient), secretInClient)
		Expect(err).NotTo(HaveOccurred())
	})

	It("caches Archive objects", func(ctx SpecContext) {
		archiveResult := &v1.Archive{}
		err := extendedClient.Get(ctx, client.ObjectKeyFromObject(archive), archiveResult)
		Expect(err).NotTo(HaveOccurred())
		Expect(extendedClient.cachedObjects).To(HaveLen(1))
		Expect(extendedClient.cachedObjects[0].entry.GetName()).To(Equal("test-object-store"))
	})

	It("distinguishes objects with same key but different types", func(ctx SpecContext) {
		// Add a Secret with the same name as the archive to the cache
		secretSameName := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-object-store",
			},
		}
		extendedClient.cachedObjects = []cachedEntry{
			{
				entry:         secretSameName,
				fetchUnixTime: time.Now().Unix(),
				ttl:           time.Duration(DefaultTTLSeconds) * time.Second,
			},
		}

		// Get the Archive with the same name - should come from base client, not from cached Secret
		archiveResult := &v1.Archive{}
		err := extendedClient.Get(ctx, client.ObjectKeyFromObject(archive), archiveResult)
		Expect(err).NotTo(HaveOccurred())
		Expect(archiveResult.Name).To(Equal("test-object-store"))
		// Should now have 2 cached entries: the Secret and the newly fetched Archive
		Expect(extendedClient.cachedObjects).To(HaveLen(2))
	})

	It("removeObject removes matching type", func(ctx SpecContext) {
		secretToRemove := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-secret",
			},
		}
		extendedClient.cachedObjects = []cachedEntry{
			{
				entry:         secretToRemove,
				fetchUnixTime: time.Now().Unix(),
				ttl:           time.Duration(DefaultTTLSeconds) * time.Second,
			},
		}

		err := extendedClient.Update(ctx, secretToRemove)
		Expect(err).NotTo(HaveOccurred())
		Expect(extendedClient.cachedObjects).To(BeEmpty())
	})

	It("removeObject removes only matching type when keys are shared", func() {
		secretSharedKey := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "shared-key",
			},
		}
		archiveSharedKey := &v1.Archive{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "shared-key",
			},
		}

		extendedClient.cachedObjects = []cachedEntry{
			{
				entry:         secretSharedKey,
				fetchUnixTime: time.Now().Unix(),
				ttl:           time.Duration(DefaultTTLSeconds) * time.Second,
			},
			{
				entry:         archiveSharedKey,
				fetchUnixTime: time.Now().Unix(),
				ttl:           time.Duration(DefaultTTLSeconds) * time.Second,
			},
		}

		extendedClient.removeObject(secretSharedKey)

		Expect(extendedClient.cachedObjects).To(HaveLen(1))
		_, isArchive := extendedClient.cachedObjects[0].entry.(*v1.Archive)
		Expect(isArchive).To(BeTrue())
		Expect(extendedClient.cachedObjects[0].entry.GetName()).To(Equal("shared-key"))
	})

	It("initializes TTL on cached entries", func(ctx SpecContext) {
		err := extendedClient.Get(ctx, client.ObjectKeyFromObject(secretInClient), secretInClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(extendedClient.cachedObjects).To(HaveLen(1))
		Expect(extendedClient.cachedObjects[0].ttl).To(Equal(time.Duration(DefaultTTLSeconds) * time.Second))
	})

	It("does not cache non-secret objects", func(ctx SpecContext) {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "test-configmap",
			},
		}
		err := extendedClient.Create(ctx, configMap)
		Expect(err).ToNot(HaveOccurred())

		err = extendedClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		Expect(err).NotTo(HaveOccurred())
		Expect(extendedClient.cachedObjects).To(BeEmpty())
	})
})
