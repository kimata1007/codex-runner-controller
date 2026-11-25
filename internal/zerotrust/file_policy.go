package zerotrust

import (
	"encoding/json"
	"io"
	"os"
	"time"
)

// FilePolicy reads a controller-owned snapshot on every check. A missing,
// malformed, oversized or expired snapshot denies traffic. Writers must use
// atomic replacement and keep the directory outside untrusted mounts.
type FilePolicy struct {
	Path   string
	Domain string
}

func (f FilePolicy) Current() *Policy {
	file, err := os.Open(f.Path)
	if err != nil {
		return nil
	}
	defer file.Close()
	const limit = 1 << 20
	b, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || len(b) > limit {
		return nil
	}
	var snapshot struct {
		Expires time.Time `json:"expires"`
		Grants  []Grant   `json:"grants"`
	}
	if err := json.Unmarshal(b, &snapshot); err != nil {
		return nil
	}
	policy, err := NewPolicy(f.Domain, snapshot.Expires, snapshot.Grants)
	if err != nil {
		return nil
	}
	return policy
}
