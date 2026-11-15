package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type KubernetesResourceClient struct {
	Client    client.Client
	Owner     client.Object
	RetainPVC bool
}

func (c KubernetesResourceClient) GetPVC(ctx context.Context, namespace, name string) (*corev1.PersistentVolumeClaim, error) {
	pvc := &corev1.PersistentVolumeClaim{}
	if err := c.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return pvc, nil
}

func (c KubernetesResourceClient) CreatePVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	if !c.RetainPVC {
		if err := controllerutil.SetControllerReference(c.Owner, pvc, c.Client.Scheme()); err != nil {
			return err
		}
	}
	return c.Client.Create(ctx, pvc)
}

func (c KubernetesResourceClient) GetService(ctx context.Context, namespace, name string) (*corev1.Service, error) {
	service := &corev1.Service{}
	if err := c.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, service); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return service, nil
}

func (c KubernetesResourceClient) CreateService(ctx context.Context, service *corev1.Service) error {
	if err := controllerutil.SetControllerReference(c.Owner, service, c.Client.Scheme()); err != nil {
		return err
	}
	return c.Client.Create(ctx, service)
}

func (c KubernetesResourceClient) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	if err := c.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, pod); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return pod, nil
}

func (c KubernetesResourceClient) CreatePod(ctx context.Context, pod *corev1.Pod) error {
	if err := controllerutil.SetControllerReference(c.Owner, pod, c.Client.Scheme()); err != nil {
		return err
	}
	return c.Client.Create(ctx, pod)
}

func (c KubernetesResourceClient) DeletePod(ctx context.Context, namespace, name string) error {
	pod, err := c.GetPod(ctx, namespace, name)
	if err != nil {
		if err == ErrNotFound {
			return ErrNotFound
		}
		return err
	}
	return c.Client.Delete(ctx, pod, &client.DeleteOptions{PropagationPolicy: ptr(metav1.DeletePropagationBackground)})
}

func ptr[T any](value T) *T { return &value }

var _ ResourceClient = KubernetesResourceClient{}
