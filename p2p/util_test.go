/**
 *  @file
 *  @copyright defined in aergo/LICENSE.txt
 */
package p2p

import (
	"fmt"
	"strconv"
	"testing"

	uuid "github.com/satori/go.uuid"

	"github.com/aergoio/aergo/pkg/log"
	addrutil "github.com/libp2p/go-addr-util"
	peer "github.com/libp2p/go-libp2p-peer"
	protocol "github.com/libp2p/go-libp2p-protocol"
	ma "github.com/multiformats/go-multiaddr"
	mnet "github.com/multiformats/go-multiaddr-net"
)

const SampleAddrString = "/ip4/172.21.11.12/tcp/3456"
const NetAddrString = "172.21.11.12:3456"

func TestGetIP(t *testing.T) {
	addrInput, _ := ma.NewMultiaddr(SampleAddrString)

	netAddr, err := mnet.ToNetAddr(addrInput)
	if err != nil {
		t.Errorf("Invalid func %s", err.Error())
	}
	fmt.Printf("net Addr : %s", netAddr.String())
	if NetAddrString != netAddr.String() {
		t.Errorf("Expected %s, but actually %s", NetAddrString, netAddr)
	}

	addrInput, _ = ma.NewMultiaddr(SampleAddrString + "/ipfs/16Uiu2HAkvvhjxVm2WE9yFBDdPQ9qx6pX9taF6TTwDNHs8VPi1EeR")
	netAddr, err = mnet.ToNetAddr(addrInput)
	if nil == err {
		t.Errorf("Error expected, but not")
	}

}
func TestLookupAddress(t *testing.T) {
	ip, err := externalIP()
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(ip)

}

func TestAddrUtil(t *testing.T) {
	addrs, err := addrutil.InterfaceAddresses()
	if err != nil {
		t.Errorf("Test Error: %s", err.Error())
	}
	fmt.Printf("Addrs : %s\n", addrs)
	fmt.Println("Over")
}

func Test_debugLogReceiveMsg(t *testing.T) {
	logger := log.NewLogger(log.TEST).WithCtx("test", "p2p")
	peerID, _ := peer.IDB58Decode("16Uiu2HAkvvhjxVm2WE9yFBDdPQ9qx6pX9taF6TTwDNHs8VPi1EeR")
	msgID := uuid.Must(uuid.NewV4()).String()
	dummyArray := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 0}
	type args struct {
		protocol   protocol.ID
		additional interface{}
	}
	tests := []struct {
		name string
		args args
	}{
		{"nil", args{pingRequest, nil}},
		{"int", args{pingResponse, len(msgID)}},
		{"pointer", args{statusRequest, &logger}},
		{"array", args{getMissingRequest, dummyArray}},
		{"string", args{getMissingResponse, "string addition"}},
		{"obj", args{pingRequest, P2P{}}},
		{"lazy", args{pingRequest, log.DoLazyEval(func() string {
			return "Length is " + strconv.Itoa(len(dummyArray))
		})}},
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnLogUnknownPeer(logger, tt.args.protocol, peerID)
			debugLogReceiveMsg(logger, tt.args.protocol, msgID, peerID, tt.args.additional)
		})
	}
}

type MockLogger struct {
}
