/**
 *  @file
 *  @copyright defined in aergo/LICENSE.txt
 */
package p2p

import (
	"net"

	"github.com/aergoio/aergo/types"
	peer "github.com/libp2p/go-libp2p-peer"
)

// PeerMeta contains non changeable information of peer node during connected state
// TODO: PeerMeta is almost same as PeerAddress, so TODO to unify them.
type PeerMeta struct {
	// IPAddress is human readable form of ip address such as "192.168.0.1" or "2001:0db8:0a0b:12f0:33:1"
	IPAddress  string
	Port       uint32
	ID         peer.ID
	Designated bool // Designated means this peer is designated in config file and connect to in startup phase
	Outbound   bool
}

// FromPeerAddress convert PeerAddress to PeerMeta
func FromPeerAddress(addr *types.PeerAddress) PeerMeta {
	meta := PeerMeta{IPAddress: net.IP(addr.Address).String(),
		Port: addr.Port, ID: peer.ID(addr.PeerID)}
	return meta
}

// ToPeerAddress convert PeerMeta to PeerAddress
func (m PeerMeta) ToPeerAddress() types.PeerAddress {
	addr := types.PeerAddress{Address: []byte(net.ParseIP(m.IPAddress)), Port: m.Port,
		PeerID: []byte(m.ID)}
	return addr
}
