package main

import (
	"encoding/json"
	"log"
	"net"
	"strings"
)

// DiscoveryPort is the UDP port the TV app broadcasts to when looking for servers.
const DiscoveryPort = 47820

const discoveryQuery = "HOMESTREAMER_DISCOVER"

// runDiscovery answers LAN broadcast queries so the TV app can find this PC
// without typing an IP address.
func runDiscovery(cfg *Config) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: DiscoveryPort})
	if err != nil {
		return err
	}
	defer conn.Close()
	reply, _ := json.Marshal(map[string]any{"app": "home-streamer", "name": cfg.Name, "port": cfg.Port, "version": version})
	buf := make([]byte, 512)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		if strings.HasPrefix(string(buf[:n]), discoveryQuery) {
			if _, err := conn.WriteToUDP(reply, from); err != nil {
				log.Printf("discovery reply to %s failed: %v", from, err)
			}
		}
	}
}
