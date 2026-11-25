package modelproxy

import (
	"context"
	"errors"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errBudgetExceeded = errors.New("model budget or rate limit exceeded")

// BudgetStore uses an optimistic Kubernetes update before making any
// upstream request. The key is the session UID, which outlives its Pods.
type BudgetStore struct {
	Kube      client.Client
	Namespace string
}

func (s BudgetStore) Reserve(ctx context.Context, uid types.UID, amount, budget, rate int64) error {
	if s.Kube == nil || s.Namespace == "" || uid == "" || amount <= 0 || budget <= 0 || rate <= 0 {
		return errors.New("model accounting is unavailable")
	}
	key := client.ObjectKey{Namespace: s.Namespace, Name: "model-" + string(uid)}
	return retry.OnError(retry.DefaultRetry, func(err error) bool {
		return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err)
	}, func() error {
		var record corev1.ConfigMap
		err := s.Kube.Get(ctx, key, &record)
		create := apierrors.IsNotFound(err)
		if err != nil && !create {
			return err
		}
		minute := strconv.FormatInt(time.Now().Unix()/60, 10)
		var used, requests int64
		if create {
			record.ObjectMeta = metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}
		} else {
			used, err = strconv.ParseInt(record.Data["used"], 10, 64)
			if err != nil || used < 0 {
				return errors.New("invalid persisted model usage")
			}
			requests, err = strconv.ParseInt(record.Data["requests"], 10, 64)
			storedMinute, clockErr := strconv.ParseInt(record.Data["minute"], 10, 64)
			if err != nil || requests < 0 || clockErr != nil || storedMinute > time.Now().Unix()/60 {
				return errors.New("invalid persisted model rate")
			}
			if record.Data["minute"] != minute {
				requests = 0
			}
		}
		if amount > budget-used || requests >= rate {
			return errBudgetExceeded
		}
		record.Data = map[string]string{"used": strconv.FormatInt(used+amount, 10), "minute": minute,
			"requests": strconv.FormatInt(requests+1, 10)}
		if create {
			return s.Kube.Create(ctx, &record)
		}
		return s.Kube.Update(ctx, &record)
	})
}

func (s BudgetStore) Refund(ctx context.Context, uid types.UID, amount int64) error {
	if amount <= 0 {
		return nil
	}
	key := client.ObjectKey{Namespace: s.Namespace, Name: "model-" + string(uid)}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var record corev1.ConfigMap
		if err := s.Kube.Get(ctx, key, &record); err != nil {
			return err
		}
		used, err := strconv.ParseInt(record.Data["used"], 10, 64)
		if err != nil || used < amount {
			return errors.New("invalid model reservation")
		}
		record.Data["used"] = strconv.FormatInt(used-amount, 10)
		return s.Kube.Update(ctx, &record)
	})
}
