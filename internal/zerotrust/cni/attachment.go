// Package cni contains the trusted host-side session attachment shared by the
// chained CNI plugin and the Node Proxy.
package cni

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
)

const (
	StateDirectory   = "/run/codex-cni"
	SocketMark       = 0xc0de0101
	ControlReplyMark = 0xc0de0102
	ProxyUID         = 0
	SocketGroup      = 65532
)

type Destination struct {
	Role      string
	VirtualIP netip.Addr
	HostPort  uint16
}

func Destinations() []Destination {
	return []Destination{
		{Role: "github", VirtualIP: netip.AddrFrom4([4]byte{198, 18, 0, 11}), HostPort: 18443},
		{Role: "web", VirtualIP: netip.AddrFrom4([4]byte{198, 18, 0, 12}), HostPort: 18444},
		{Role: "model", VirtualIP: netip.AddrFrom4([4]byte{198, 18, 0, 13}), HostPort: 18445},
	}
}

type Attachment struct {
	Namespace  string     `json:"namespace"`
	PodName    string     `json:"podName"`
	PodUID     string     `json:"podUID"`
	Generation string     `json:"generation"`
	Device     string     `json:"device"`
	IfIndex    int        `json:"ifindex"`
	PeerIndex  int        `json:"peerIndex"`
	NetNS      string     `json:"netns"`
	NetNSInode uint64     `json:"netnsInode"`
	PodIP      netip.Addr `json:"podIP"`
	HostIP     netip.Addr `json:"hostIP"`
}

var (
	containerGeneration = regexp.MustCompile(`^[a-f0-9]{32}$`)
	podUID              = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)
)

func Generation(container string) string {
	digest := sha256.Sum256([]byte(container))
	return hex.EncodeToString(digest[:16])
}

func (a Attachment) Valid() bool {
	return containerGeneration.MatchString(a.Generation) && podUID.MatchString(a.PodUID) &&
		a.Namespace != "" && a.PodName != "" && a.Device != "" && a.IfIndex > 0 &&
		a.PeerIndex > 0 && a.NetNSInode != 0 && filepath.IsAbs(a.NetNS) &&
		a.PodIP.Is4() && a.PodIP.IsGlobalUnicast() && a.HostIP.Is4() && a.HostIP.IsGlobalUnicast() && a.HostIP != a.PodIP
}

func (a Attachment) Alias() string {
	return "codex-cni:" + a.PodUID + ":" + a.Generation
}

func WriteAttachment(directory string, a Attachment) error {
	if !a.Valid() {
		return errors.New("invalid CNI attachment")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".attachment-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := json.NewEncoder(f).Encode(a); err != nil {
		return err
	}
	if err := f.Chmod(0644); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(directory, a.Generation+".json"))
}

func ReadAttachment(directory, generation string) (Attachment, error) {
	if !containerGeneration.MatchString(generation) {
		return Attachment{}, errors.New("invalid CNI generation")
	}
	file, err := os.Open(filepath.Join(directory, generation+".json"))
	if err != nil {
		return Attachment{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 || info.Mode().Perm()&0022 != 0 {
		return Attachment{}, errors.New("invalid CNI attachment file")
	}
	var a Attachment
	d := json.NewDecoder(io.LimitReader(file, 4097))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil || d.Decode(new(json.RawMessage)) != io.EOF || !a.Valid() || a.Generation != generation {
		return Attachment{}, errors.New("invalid CNI attachment record")
	}
	return a, nil
}

func ReadAttachments(directory string) ([]Attachment, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	results := make([]Attachment, 0, len(entries))
	for _, entry := range entries {
		if len(entry.Name()) != 37 || filepath.Ext(entry.Name()) != ".json" || !entry.Type().IsRegular() {
			continue
		}
		if a, err := ReadAttachment(directory, entry.Name()[:32]); err == nil {
			results = append(results, a)
		}
	}
	return results, nil
}
