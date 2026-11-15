package kubernetes

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const ChatGPTAuthSecretName = "codex-runner-chatgpt-auth"

type ChatGPTAuthSecretStore struct {
	Client    ctrlclient.Client
	Namespace string
}

func (s ChatGPTAuthSecretStore) Read(ctx context.Context) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := s.Client.Get(ctx, types.NamespacedName{Namespace: s.Namespace, Name: ChatGPTAuthSecretName}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ChatGPT credential secret: %w", err)
	}
	return append([]byte(nil), secret.Data["credential"]...), nil
}

func (s ChatGPTAuthSecretStore) Write(ctx context.Context, credential []byte) error {
	key := types.NamespacedName{Namespace: s.Namespace, Name: ChatGPTAuthSecretName}
	secret := &corev1.Secret{}
	err := s.Client.Get(ctx, key, secret)
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("ChatGPT credential secret %q must be created by the deployment", key.Name)
	}
	if err != nil {
		return fmt.Errorf("read ChatGPT credential secret: %w", err)
	}
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
	}
	secret.Data["credential"] = append([]byte(nil), credential...)
	if err := s.Client.Update(ctx, secret); err != nil {
		return fmt.Errorf("update ChatGPT credential secret: %w", err)
	}
	return nil
}

func (s ChatGPTAuthSecretStore) Delete(ctx context.Context) error {
	return s.Write(ctx, nil)
}
