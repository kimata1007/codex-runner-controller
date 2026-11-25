package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/kimata1007/codex-runner-controller/internal/zerotrust/cni"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type pluginConfig struct {
	types.NetConf
	SessionNamespace string `json:"sessionNamespace"`
	StateDirectory   string `json:"stateDirectory"`
	NodeIP           string `json:"nodeIP"`
}

type kubernetesArgs struct {
	types.CommonArgs
	K8S_POD_NAMESPACE types.UnmarshallableString
	K8S_POD_NAME      types.UnmarshallableString
	K8S_POD_UID       types.UnmarshallableString
}

func main() {
	skel.PluginMainFuncs(skel.CNIFuncs{Add: add, Del: del, Check: check},
		version.PluginSupports("0.3.1", "0.4.0", "1.0.0", "1.1.0"), "codex-session-isolation")
}

func load(args *skel.CmdArgs) (pluginConfig, kubernetesArgs, error) {
	config := pluginConfig{SessionNamespace: "codex-sessions", StateDirectory: cni.StateDirectory}
	var pod kubernetesArgs
	if err := json.Unmarshal(args.StdinData, &config); err != nil {
		return config, pod, err
	}
	if err := types.LoadArgs(args.Args, &pod); err != nil {
		return config, pod, err
	}
	if config.SessionNamespace == "" || !filepath.IsAbs(config.StateDirectory) || pod.K8S_POD_NAMESPACE == "" || args.ContainerID == "" {
		return config, pod, errors.New("incomplete session isolation configuration")
	}
	return config, pod, nil
}

func add(args *skel.CmdArgs) error {
	config, pod, err := load(args)
	if err != nil {
		return err
	}
	if err := version.ParsePrevResult(&config.NetConf); err != nil || config.PrevResult == nil {
		return errors.New("session isolation must follow the primary veth plugin")
	}
	if string(pod.K8S_POD_NAMESPACE) != config.SessionNamespace {
		return types.PrintResult(config.PrevResult, config.CNIVersion)
	}
	result, err := current.NewResultFromResult(config.PrevResult)
	if err != nil {
		return err
	}
	ip, err := delegatedAddress(result, args.Netns, args.IfName)
	if err != nil {
		return err
	}
	host, err := nodeIPv4(config.NodeIP)
	if err != nil {
		return err
	}
	generation := cni.Generation(args.ContainerID)
	return withLock(config.StateDirectory, generation, func() error {
		attachment, err := cni.Identify(cni.Attachment{
			Namespace: config.SessionNamespace, PodName: string(pod.K8S_POD_NAME), PodUID: string(pod.K8S_POD_UID),
			Generation: generation, NetNS: args.Netns, PodIP: ip, HostIP: host,
		}, args.IfName)
		if err != nil {
			return err
		}
		if err := attachment.Install(); err != nil {
			return errors.Join(err, attachment.Detach())
		}
		if err := cni.WriteAttachment(config.StateDirectory, attachment); err != nil {
			return errors.Join(err, removeAttachment(config.StateDirectory, attachment))
		}
		return types.PrintResult(config.PrevResult, config.CNIVersion)
	})
}

func del(args *skel.CmdArgs) error {
	config, pod, err := load(args)
	if err != nil {
		return err
	}
	if string(pod.K8S_POD_NAMESPACE) != config.SessionNamespace {
		return nil
	}
	generation := cni.Generation(args.ContainerID)
	return withLock(config.StateDirectory, generation, func() error {
		attachment, err := cni.ReadAttachment(config.StateDirectory, generation)
		if errors.Is(err, os.ErrNotExist) {
			// Without a link record the firewall cannot be safely removed.
			return nil
		}
		if err != nil {
			return err
		}
		return removeAttachment(config.StateDirectory, attachment)
	})
}

func removeAttachment(directory string, attachment cni.Attachment) error {
	// Tear down the veth before removing filters. DEL can be called before
	// the delegated plugin or while a sandbox still holds its namespace.
	if err := attachment.Detach(); err != nil {
		return err
	}
	if err := cni.RemoveFirewall(attachment.Generation); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(directory, attachment.Generation+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func check(args *skel.CmdArgs) error {
	config, pod, err := load(args)
	if err != nil {
		return err
	}
	if string(pod.K8S_POD_NAMESPACE) != config.SessionNamespace {
		return nil
	}
	attachment, err := cni.ReadAttachment(config.StateDirectory, cni.Generation(args.ContainerID))
	if err != nil || attachment.PodUID != string(pod.K8S_POD_UID) || attachment.NetNS != args.Netns || !attachment.Live() {
		return errors.New("session attachment changed")
	}
	return cni.CheckFirewall(attachment.Generation)
}

func withLock(directory, generation string, operation func() error) error {
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	// Serialize this short install/teardown transaction across CNI processes.
	lock, err := os.OpenFile(filepath.Join(directory, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock CNI attachment %s: %w", generation, err)
	}
	return operation()
}

func delegatedAddress(result *current.Result, namespace, ifname string) (netip.Addr, error) {
	var address netip.Addr
	for _, ip := range result.IPs {
		if ip.Interface == nil || *ip.Interface < 0 || *ip.Interface >= len(result.Interfaces) {
			continue
		}
		iface := result.Interfaces[*ip.Interface]
		if iface.Name != ifname || iface.Sandbox != namespace {
			continue
		}
		v4, ok := netip.AddrFromSlice(ip.Address.IP.To4())
		if !ok {
			continue
		}
		if address.IsValid() {
			return netip.Addr{}, errors.New("multiple delegated IPv4 addresses are unsupported")
		}
		address = v4
	}
	if !address.IsValid() {
		return netip.Addr{}, errors.New("delegated IPv4 address is missing")
	}
	return address, nil
}

func nodeIPv4(configured string) (netip.Addr, error) {
	var candidate netip.Addr
	if configured != "" {
		candidate, _ = netip.ParseAddr(configured)
	} else {
		routes, err := netlink.RouteGet(net.IP(cni.Destinations()[0].VirtualIP.AsSlice()))
		if err != nil || len(routes) != 1 {
			return netip.Addr{}, errors.New("local proxy address is unavailable")
		}
		candidate, _ = netip.AddrFromSlice(routes[0].Src.To4())
	}
	if !candidate.Is4() || !candidate.IsGlobalUnicast() {
		return netip.Addr{}, errors.New("local proxy address must be IPv4 unicast")
	}
	local, err := netlink.AddrList(nil, netlink.FAMILY_V4)
	if err != nil {
		return netip.Addr{}, err
	}
	for _, iface := range local {
		if ip, ok := netip.AddrFromSlice(iface.IP.To4()); ok && ip == candidate {
			return ip, nil
		}
	}
	return netip.Addr{}, errors.New("proxy address is not assigned to this node")
}
