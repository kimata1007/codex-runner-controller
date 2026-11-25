package cni

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const (
	proxyPort      uint16 = 8080
	controlPort    uint16 = 8081
	destinationNAT        = uint32(1 << 5)
)

func firewallName(generation string) string {
	return "codex_" + generation
}

// Install installs all rules for a veth in one netfilter transaction.
func (a Attachment) Install() error {
	if !a.Live() {
		return errors.New("CNI link is unavailable")
	}
	conn, table, err := findFirewall(a.Generation)
	if err != nil {
		return err
	}
	if table != nil {
		conn.DelTable(table)
	}
	table = conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: firewallName(a.Generation)})
	prerouting := chain(conn, table, "route", nftables.ChainHookPrerouting, nftables.ChainTypeNAT, -110)
	input := chain(conn, table, "in", nftables.ChainHookInput, nftables.ChainTypeFilter, -80)
	output := chain(conn, table, "out", nftables.ChainHookOutput, nftables.ChainTypeFilter, -80)
	forward := chain(conn, table, "forward", nftables.ChainHookForward, nftables.ChainTypeFilter, -80)

	for _, route := range Destinations() {
		nat := a.fromPod()
		nat = append(nat, ipv4Address(16, route.VirtualIP)...)
		nat = append(nat, tcpPort(2, proxyPort)...)
		nat = append(nat, &expr.Immediate{Register: 1, Data: addressBytes(a.HostIP)},
			&expr.Immediate{Register: 2, Data: portBytes(route.HostPort)},
			&expr.NAT{Type: expr.NATTypeDestNAT, Family: unix.NFPROTO_IPV4, RegAddrMin: 1, RegProtoMin: 2})
		addRule(conn, table, prerouting, nat)

		proxy := a.fromPod()
		proxy = append(proxy, ipv4Address(16, a.HostIP)...)
		proxy = append(proxy, tcpPort(2, route.HostPort)...)
		proxy = append(proxy, maskedConnection(expr.CtKeySTATUS, destinationNAT)...)
		established := append(append([]expr.Any(nil), proxy...), maskedConnection(expr.CtKeySTATE, expr.CtStateBitESTABLISHED)...)
		established = append(established, &expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: native32(ProxyFlowMark)}, verdict(expr.VerdictAccept))
		addRule(conn, table, input, established)
		first := append(proxy, &expr.Socket{Key: expr.SocketKeyMark, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: native32(SocketMark)},
			&expr.Immediate{Register: 1, Data: native32(ProxyFlowMark)},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true}, verdict(expr.VerdictAccept))
		addRule(conn, table, input, first)
	}

	reply := append(a.fromPod(), &expr.Ct{Key: expr.CtKeyMARK, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: native32(ControlReplyMark)}, verdict(expr.VerdictAccept))
	addRule(conn, table, input, reply)
	addRule(conn, table, input, append(a.interfaceMatch(expr.MetaKeyIIF), verdict(expr.VerdictDrop)))

	toPod := a.interfaceMatch(expr.MetaKeyOIF)
	toPod = append(toPod, ipv4TCP()...)
	toPod = append(toPod, ipv4Address(16, a.PodIP)...)
	toPod = append(toPod, &expr.Meta{Key: expr.MetaKeySKUID, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: native32(ProxyUID)},
		&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: native32(SocketMark)})
	start := append(append([]expr.Any(nil), toPod...), tcpPort(2, controlPort)...)
	start = append(start, &expr.Immediate{Register: 1, Data: native32(ControlReplyMark)},
		&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true}, verdict(expr.VerdictAccept))
	addRule(conn, table, output, start)
	toPod = append(toPod, maskedConnection(expr.CtKeySTATE, expr.CtStateBitESTABLISHED)...)
	addRule(conn, table, output, append(toPod, verdict(expr.VerdictAccept)))
	addRule(conn, table, output, append(a.interfaceMatch(expr.MetaKeyOIF), verdict(expr.VerdictDrop)))

	for _, direction := range []expr.MetaKey{expr.MetaKeyIIF, expr.MetaKeyOIF} {
		addRule(conn, table, forward, append(a.interfaceMatch(direction), verdict(expr.VerdictDrop)))
	}
	return conn.Flush()
}

func CheckFirewall(generation string) error {
	conn, table, err := findFirewall(generation)
	if err != nil {
		return err
	}
	if table == nil {
		return errors.New("session firewall is unavailable")
	}
	for name, minimum := range map[string]int{"route": 3, "in": 8, "out": 3, "forward": 2} {
		chain, err := conn.ListChain(table, name)
		if err != nil {
			return err
		}
		rules, err := conn.GetRules(table, chain)
		if err != nil || len(rules) != minimum {
			return errors.New("session firewall rules are unavailable")
		}
	}
	return nil
}

func RemoveFirewall(generation string) error {
	conn, table, err := findFirewall(generation)
	if err != nil || table == nil {
		return err
	}
	conn.DelTable(table)
	return conn.Flush()
}

func findFirewall(generation string) (*nftables.Conn, *nftables.Table, error) {
	if !containerGeneration.MatchString(generation) {
		return nil, nil, errors.New("invalid session firewall generation")
	}
	conn := &nftables.Conn{}
	tables, err := conn.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return nil, nil, err
	}
	for _, table := range tables {
		if table.Name == firewallName(generation) {
			return conn, table, nil
		}
	}
	return conn, nil, nil
}

func chain(conn *nftables.Conn, table *nftables.Table, name string, hook *nftables.ChainHook, kind nftables.ChainType, priority nftables.ChainPriority) *nftables.Chain {
	return conn.AddChain(&nftables.Chain{Table: table, Name: name, Hooknum: hook,
		Type: kind, Priority: nftables.ChainPriorityRef(priority)})
}

func addRule(conn *nftables.Conn, table *nftables.Table, ch *nftables.Chain, rules []expr.Any) {
	conn.AddRule(&nftables.Rule{Table: table, Chain: ch, Exprs: rules})
}

func (a Attachment) fromPod() []expr.Any {
	match := append(a.interfaceMatch(expr.MetaKeyIIF), ipv4TCP()...)
	return append(match, ipv4Address(12, a.PodIP)...)
}

func (a Attachment) interfaceMatch(direction expr.MetaKey) []expr.Any {
	return []expr.Any{&expr.Meta{Key: direction, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: native32(uint32(a.IfIndex))}}
}

func ipv4TCP() []expr.Any {
	return []expr.Any{&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.NFPROTO_IPV4}},
		&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_TCP}}}
}

func ipv4Address(offset uint32, address netip.Addr) []expr.Any {
	return []expr.Any{&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: offset, Len: 4},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: addressBytes(address)}}
}

func tcpPort(offset uint32, port uint16) []expr.Any {
	return []expr.Any{&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: offset, Len: 2},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: portBytes(port)}}
}

func maskedConnection(key expr.CtKey, bit uint32) []expr.Any {
	return []expr.Any{&expr.Ct{Key: key, Register: 1},
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: native32(bit), Xor: bytes.Repeat([]byte{0}, 4)},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: native32(bit)}}
}

func verdict(kind expr.VerdictKind) *expr.Verdict {
	return &expr.Verdict{Kind: kind}
}

func addressBytes(address netip.Addr) []byte {
	ip := address.As4()
	return ip[:]
}

func portBytes(port uint16) []byte {
	return binary.BigEndian.AppendUint16(nil, port)
}

func native32(value uint32) []byte {
	return binary.NativeEndian.AppendUint32(nil, value)
}
