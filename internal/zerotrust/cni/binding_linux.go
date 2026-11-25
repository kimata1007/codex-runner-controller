package cni

import (
	"errors"
	"net/netip"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.org/x/sys/unix"
)

// Identify resolves the network namespace and its veth pair from the runtime
// attachment, not from the Pod's IP or any input provided by the workload.
func Identify(a Attachment, podInterface string) (Attachment, error) {
	handle, link, inode, err := inside(a.NetNS, podInterface)
	if err != nil {
		return Attachment{}, err
	}
	defer handle.Close()
	if link.Type() != "veth" || link.Attrs().ParentIndex <= 0 {
		return Attachment{}, errors.New("session requires point-to-point veth")
	}
	a.PeerIndex, a.NetNSInode = link.Attrs().Index, inode
	host, err := netlink.LinkByIndex(link.Attrs().ParentIndex)
	if err != nil || !matchesHost(host, a.PeerIndex) {
		return Attachment{}, errors.New("session veth has no matching unbridged host peer")
	}
	a.IfIndex, a.Device = host.Attrs().Index, host.Attrs().Name
	addresses, err := handle.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return Attachment{}, err
	}
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if ok && ip.Unmap() == a.PodIP {
			if !a.Valid() || (host.Attrs().Alias != "" && host.Attrs().Alias != a.Alias()) {
				return Attachment{}, errors.New("session veth attachment differs from the delegated result")
			}
			return a, netlink.LinkSetAlias(host, a.Alias())
		}
	}
	return Attachment{}, errors.New("session veth is missing the delegated Pod address")
}

func (a Attachment) Live() bool {
	var ns unix.Stat_t
	if !a.Valid() || unix.Stat(a.NetNS, &ns) != nil || ns.Ino != a.NetNSInode {
		return false
	}
	host, err := netlink.LinkByIndex(a.IfIndex)
	return err == nil && matchesHost(host, a.PeerIndex) &&
		host.Attrs().Name == a.Device && host.Attrs().Alias == a.Alias()
}

func (a Attachment) Detach() error {
	host, err := netlink.LinkByIndex(a.IfIndex)
	if err != nil {
		var missing netlink.LinkNotFoundError
		if errors.As(err, &missing) {
			return nil
		}
		return err
	}
	if !matchesHost(host, a.PeerIndex) || host.Attrs().Name != a.Device || host.Attrs().Alias != a.Alias() {
		return nil
	}
	return netlink.LinkDel(host)
}

func matchesHost(link netlink.Link, peer int) bool {
	return link != nil && link.Type() == "veth" && link.Attrs().MasterIndex == 0 && link.Attrs().ParentIndex == peer
}

func inside(path, name string) (*netlink.Handle, netlink.Link, uint64, error) {
	ns, err := netns.GetFromPath(path)
	if err != nil {
		return nil, nil, 0, err
	}
	defer ns.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(int(ns), &stat); err != nil {
		return nil, nil, 0, err
	}
	handle, err := netlink.NewHandleAt(ns, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, nil, 0, err
	}
	link, err := handle.LinkByName(name)
	if err != nil {
		handle.Close()
		return nil, nil, 0, err
	}
	return handle, link, stat.Ino, nil
}
