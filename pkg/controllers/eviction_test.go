package controllers

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("EvictPod", func() {
	var (
		ctx    context.Context
		pod    *corev1.Pod
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(policyv1.AddToScheme(scheme)).To(Succeed())

		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
			Spec: corev1.PodSpec{
				TerminationGracePeriodSeconds: ptr.To[int64](30),
			},
		}
	})

	Context("when eviction succeeds", func() {
		It("should return EvictionResultEvicted", func() {
			// Intercept SubResource().Create() and return success
			funcs := interceptor.Funcs{
				SubResourceCreate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
					return nil
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				WithInterceptorFuncs(funcs).
				Build()

			result, err := EvictPod(ctx, fakeClient, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(EvictionResultEvicted))
		})
	})

	Context("when pod is not found", func() {
		It("should return EvictionResultAlreadyGone", func() {
			funcs := interceptor.Funcs{
				SubResourceCreate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
					return apierrors.NewNotFound(schema.GroupResource{Group: "", Resource: "pods"}, "test-pod")
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(funcs).
				Build()

			result, err := EvictPod(ctx, fakeClient, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(EvictionResultAlreadyGone))
		})
	})

	Context("when PDB blocks eviction", func() {
		It("should return EvictionResultPDBBlocked with error", func() {
			funcs := interceptor.Funcs{
				SubResourceCreate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
					return apierrors.NewTooManyRequests("Cannot evict pod as it would violate the pod's disruption budget", 0)
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				WithInterceptorFuncs(funcs).
				Build()

			result, err := EvictPod(ctx, fakeClient, pod)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(EvictionResultPDBBlocked))
			Expect(apierrors.IsTooManyRequests(err)).To(BeTrue())
		})
	})

	Context("when unexpected error occurs", func() {
		It("should return EvictionResultError with error", func() {
			funcs := interceptor.Funcs{
				SubResourceCreate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
					return apierrors.NewBadRequest("unexpected error")
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				WithInterceptorFuncs(funcs).
				Build()

			result, err := EvictPod(ctx, fakeClient, pod)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(EvictionResultError))
		})
	})
})

var _ = Describe("CanEvict", func() {
	var (
		ctx    context.Context
		pod    *corev1.Pod
		scheme *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(policyv1.AddToScheme(scheme)).To(Succeed())

		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
			},
		}
	})

	Context("when dry-run succeeds", func() {
		It("should return true without error", func() {
			funcs := interceptor.Funcs{
				SubResourceCreate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
					return nil
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				WithInterceptorFuncs(funcs).
				Build()

			canEvict, err := CanEvict(ctx, fakeClient, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(canEvict).To(BeTrue())
		})
	})

	Context("when pod is not found", func() {
		It("should return false with error", func() {
			funcs := interceptor.Funcs{
				SubResourceCreate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
					return apierrors.NewNotFound(schema.GroupResource{}, "pod")
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithInterceptorFuncs(funcs).
				Build()

			canEvict, err := CanEvict(ctx, fakeClient, pod)
			Expect(err).To(HaveOccurred())
			Expect(canEvict).To(BeFalse())
		})
	})

	Context("when PDB would block", func() {
		It("should return false without error", func() {
			funcs := interceptor.Funcs{
				SubResourceCreate: func(ctx context.Context, c client.Client, subResourceName string, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
					return apierrors.NewTooManyRequests("PDB would block", 0)
				},
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(pod).
				WithInterceptorFuncs(funcs).
				Build()

			canEvict, err := CanEvict(ctx, fakeClient, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(canEvict).To(BeFalse())
		})
	})
})

var _ = Describe("EvictionError", func() {
	Context("NewEvictionBlockedError", func() {
		It("should create proper error with wrapping", func() {
			originalErr := apierrors.NewTooManyRequests("", 0)
			err := NewEvictionBlockedError("test-pod", "PDB blocking", originalErr)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("test-pod"))
			Expect(err.Error()).To(ContainSubstring("PDB blocking"))
			Expect(IsEvictionBlocked(err)).To(BeTrue())
		})
	})

	Context("IsEvictionBlocked", func() {
		It("should identify eviction errors", func() {
			evictErr := NewEvictionBlockedError("pod", "reason", nil)
			Expect(IsEvictionBlocked(evictErr)).To(BeTrue())
		})

		It("should not identify other errors", func() {
			otherErr := apierrors.NewNotFound(schema.GroupResource{}, "pod")
			Expect(IsEvictionBlocked(otherErr)).To(BeFalse())
		})
	})
})
