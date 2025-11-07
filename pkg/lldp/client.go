/*
MIT License

Copyright (c) 2020 The Metal-Stack Authors.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package lldp

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

const (
	// Make use of an LLDP EtherType.
	// https://www.iana.org/assignments/ieee-802-numbers/ieee-802-numbers.xhtml
	etherType = 0x88cc
)

// Client consumes lldp messages.
type Client struct {
	InterfaceName string
	InterfaceMac  []byte
	handle        *pcap.Handle
	ctx           context.Context
}

// DiscoveryResult holds optional TLV SysName and SysDescription fields of a real lldp frame.
type DiscoveryResult struct {
	InterfaceName   string
	SysName         string
	SysDescription  string
	PortDescription string
	PeerMAC         []byte
}

// NewClient creates a new lldp client.
func NewClient(ctx context.Context, ifacename string, hwAddr []byte) *Client {
	return &Client{
		InterfaceName: ifacename,
		InterfaceMac:  hwAddr,
		ctx:           ctx,
	}
}

// Start searches on the configured interface for lldp packages and
// pushes the optional TLV SysName and SysDescription fields of each
// found lldp package into the given channel.
func (l *Client) Start(resultChan chan<- DiscoveryResult, portDescriptionFilter func(string) bool) error {
	defer l.Close()

	var packetSource *gopacket.PacketSource
	for {
		// Recreate interface handle if not exists
		if l.handle == nil {
			var err error
			l.handle, err = pcap.OpenLive(l.InterfaceName, 65536, true, 5*time.Second)
			if err != nil {
				return fmt.Errorf("unable to open interface:%s in promiscuous mode: %w", l.InterfaceName, err)
			}

			// filter only lldp packages
			bpfFilter := fmt.Sprintf("ether proto %#x", etherType)
			err = l.handle.SetBPFFilter(bpfFilter)
			if err != nil {
				return fmt.Errorf("unable to filter lldp ethernet traffic %#x on interface:%s %w", etherType, l.InterfaceName, err)
			}

			packetSource = gopacket.NewPacketSource(l.handle, l.handle.LinkType())
		}

		select {
		case packet, ok := <-packetSource.Packets():
			if !ok {
				l.handle.Close()
				l.handle = nil
				continue
			}

			if packet.LinkLayer().LayerType() != layers.LayerTypeEthernet {
				continue
			}

			// Ignore LLDP packets sent by us
			if reflect.DeepEqual(packet.LinkLayer().LinkFlow().Src().Raw(), l.InterfaceMac) {
				continue
			}

			infoFound := false

			dr := DiscoveryResult{InterfaceName: l.InterfaceName}
			for _, layer := range packet.Layers() {
				if layer.LayerType() == layers.LayerTypeLinkLayerDiscovery {
					info, ok := layer.(*layers.LinkLayerDiscovery)
					if !ok {
						continue
					}

					if info.ChassisID.Subtype == layers.LLDPChassisIDSubTypeMACAddr {
						dr.PeerMAC = info.ChassisID.ID
					}

					if info.PortID.Subtype == layers.LLDPPortIDSubtypeMACAddr {
						dr.PeerMAC = info.PortID.ID
					}

					continue
				}

				if layer.LayerType() == layers.LayerTypeLinkLayerDiscoveryInfo {
					info, ok := layer.(*layers.LinkLayerDiscoveryInfo)
					if !ok {
						continue
					}

					if portDescriptionFilter != nil && !portDescriptionFilter(info.PortDescription) {
						// Filter function did not match, ignore this packet
						continue
					}

					dr.SysName = info.SysName
					dr.SysDescription = info.SysDescription
					dr.PortDescription = info.PortDescription

					infoFound = true
				}

			}

			if !infoFound {
				// We did not find the info layer, ignore this packet
				continue
			}

			resultChan <- dr
			return nil

		case <-l.ctx.Done():
			return nil
		}
	}
}

// Close the LLDP client
func (l *Client) Close() {
	if l.handle != nil {
		l.handle.Close()
	}
}
