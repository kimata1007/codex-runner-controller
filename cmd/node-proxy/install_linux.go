package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/kimata1007/codex-runner-controller/internal/zerotrust/cni"
)

func installCNI(configPath, state, local, namespace string) error {
	if !filepath.IsAbs(configPath) || !filepath.IsAbs(state) || !filepath.IsAbs(local) || namespace == "" {
		return errors.New("invalid CNI installation paths")
	}
	if err := os.MkdirAll(state, 0755); err != nil {
		return err
	}
	for _, dir := range []string{local, filepath.Join(local, "github"), filepath.Join(local, "web"), filepath.Join(local, "model")} {
		mode := os.FileMode(0770)
		if dir == local {
			mode = 0710
		}
		if err := os.MkdirAll(dir, mode); err != nil {
			return err
		}
		if err := os.Chown(dir, cni.ProxyUID, cni.SocketGroup); err != nil {
			return err
		}
		if err := os.Chmod(dir, mode|os.ModeSetgid); err != nil {
			return err
		}
	}
	cniExecutable, err := os.ReadFile("/codex-session-cni")
	if err != nil {
		return err
	}
	if err := writeAtomic("/host-cni-bin/codex-session-cni", cniExecutable, 0755); err != nil {
		return err
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	next, err := appendIsolation(config, state, namespace)
	if err != nil {
		return err
	}
	if next == nil {
		return nil
	}
	return writeAtomic(configPath, next, 0644)
}

func appendIsolation(data []byte, state, namespace string) ([]byte, error) {
	var config map[string]json.RawMessage
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	var plugins []map[string]json.RawMessage
	if err := json.Unmarshal(config["plugins"], &plugins); err != nil || len(plugins) == 0 {
		return nil, errors.New("CNI configuration must contain the primary plugin chain")
	}
	var version string
	if err := json.Unmarshal(config["cniVersion"], &version); err != nil {
		return nil, err
	}
	switch version {
	case "0.3.1", "0.4.0", "1.0.0", "1.1.0":
	default:
		return nil, errors.New("unsupported CNI configuration version")
	}
	entry, err := json.Marshal(map[string]string{"type": "codex-session-cni", "stateDirectory": state, "sessionNamespace": namespace})
	if err != nil {
		return nil, err
	}
	for i, plugin := range plugins {
		var kind string
		if err := json.Unmarshal(plugin["type"], &kind); err != nil {
			return nil, err
		}
		if i == 0 && kind != "ptp" {
			return nil, errors.New("the primary CNI must provide unbridged point-to-point veth")
		}
		if kind == "codex-session-cni" {
			if i != len(plugins)-1 {
				return nil, errors.New("session isolation must be the final CNI plugin")
			}
			old, _ := json.Marshal(plugin)
			if string(old) == string(entry) {
				return nil, nil
			}
			return nil, errors.New("existing session CNI plugin configuration differs")
		}
	}
	var plugin map[string]json.RawMessage
	if err := json.Unmarshal(entry, &plugin); err != nil {
		return nil, err
	}
	config["plugins"], err = json.Marshal(append(plugins, plugin))
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(config, "", "  ")
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".codex-install-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
