package scan

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type Candidate struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// ScanSubnet scans the local subnet for devices with port 22 open.
// It assumes a /24 subnet based on the local IP.
func ScanSubnet() ([]Candidate, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var localIP net.IP

	// Find a non-loopback IPv4 address
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				localIP = ipnet.IP
				break
			}
		}
	}

	if localIP == nil {
		return nil, fmt.Errorf("no local IP found")
	}

	// Calculate subnet range (assuming /24 for simplicity in this starter)
	// A robust implementation would iterate the full mask.
	ipv4 := localIP.To4()
	baseIP := net.IPv4(ipv4[0], ipv4[1], ipv4[2], 0)

	candidates := []Candidate{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Scan 1-254
	for i := 1; i < 255; i++ {
		ip := net.IPv4(baseIP[12], baseIP[13], baseIP[14], byte(i))
		if ip.Equal(localIP) {
			continue
		}

		wg.Add(1)
		go func(targetIP string) {
			defer wg.Done()
			address := fmt.Sprintf("%s:22", targetIP)
			conn, err := net.DialTimeout("tcp", address, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				mu.Lock()
				candidates = append(candidates, Candidate{IP: targetIP, Port: 22})
				mu.Unlock()
			}
		}(ip.String())
	}

	wg.Wait()
	return candidates, nil
}
