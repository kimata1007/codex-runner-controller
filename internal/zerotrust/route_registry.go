package zerotrust

import (
	"context"
	"errors"
	"sync"
)

// RouteRegistry owns the lifetime of local targets established from trusted
// enrollment. Removing a binding cancels connections using that target.
type RouteRegistry struct {
	mu      sync.RWMutex
	targets map[string]map[CertificateBinding]LocalTarget
}

func (r *RouteRegistry) Register(ctx context.Context, target LocalTarget) (func(), error) {
	binding := target.Destination
	if binding.Identity == "" || binding.PodUID == "" || target.Port == 0 || target.Connect == nil || ctx.Err() != nil {
		return nil, errors.New("invalid local route target")
	}
	name := TargetName(binding.Identity, target.Port)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.targets == nil {
		r.targets = make(map[string]map[CertificateBinding]LocalTarget)
	}
	replicas := r.targets[name]
	if replicas == nil {
		replicas = make(map[CertificateBinding]LocalTarget)
		r.targets[name] = replicas
	}
	if _, exists := replicas[binding]; exists {
		return nil, errors.New("local route target is already registered")
	}
	lifetime, cancel := context.WithCancel(ctx)
	target.Lifetime = lifetime
	replicas[binding] = target
	var once sync.Once
	remove := func() {
		once.Do(func() {
			cancel()
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(replicas, binding)
			if len(replicas) == 0 {
				delete(r.targets, name)
			}
		})
	}
	return remove, nil
}

func (r *RouteRegistry) Resolve(name string) (LocalTarget, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, target := range r.targets[name] {
		if target.Lifetime.Err() == nil {
			return target, true
		}
	}
	return LocalTarget{}, false
}
