package scan

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Candidate struct {
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	MAC          string `json:"mac"`
	Manufacturer string `json:"manufacturer"`
	Banner       string `json:"banner,omitempty"`
}

var defaultTurtlebotPrefixes = []string{
	"28:CD:C1", "2C:CF:67", "B8:27:EB", "D8:3A:DD", "DC:A6:32", "E4:5F:01", "3A:35:41",
}

func getMACPrefixes() []string {
	env := os.Getenv("TURTLEBOT_MAC_PREFIXES")
	if env == "" {
		return defaultTurtlebotPrefixes
	}
	return append(defaultTurtlebotPrefixes, strings.Split(env, ",")...)
}

func getARPTable() map[string]string {
	arpTable := make(map[string]string)

	// Try Linux /proc/net/arp first
	data, err := os.ReadFile("/proc/net/arp")
	if err == nil {
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if i == 0 {
				continue // Skip header
			}
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				ip := fields[0]
				mac := fields[3]
				if mac != "00:00:00:00:00:00" {
					arpTable[ip] = mac
				}
			}
		}
		return arpTable
	}

	// Fallback to arp command (macOS/BSD)
	cmd := exec.Command("arp", "-an")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[scan] arp command failed: %v", err)
		return arpTable
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// Format: ? (192.168.1.1) at 00:11:22:33:44:55 on en0 ...
		if strings.Contains(line, "(") && strings.Contains(line, ") at ") {
			parts := strings.Fields(line)
			// parts[1] is (IP), parts[3] is MAC
			if len(parts) >= 4 {
				ip := strings.Trim(parts[1], "()")
				macStr := parts[3]

				// Normalize MAC
				hw, err := net.ParseMAC(macStr)
				if err == nil {
					arpTable[ip] = hw.String()
				} else {
					arpTable[ip] = macStr
				}
			}
		}
	}

	return arpTable
}

func isTurtlebot(mac string) bool {
	mac = strings.ToUpper(mac)
	for _, prefix := range getMACPrefixes() {
		cleanPrefix := strings.ReplaceAll(strings.ToUpper(prefix), ":", "")
		cleanMAC := strings.ReplaceAll(mac, ":", "")
		if strings.HasPrefix(cleanMAC, cleanPrefix) {
			return true
		}
	}
	return false
}

// ScanSubnet scans all local subnets for devices with port 22 open.
// It identifies all non-loopback IPv4 interfaces and scans their /24 ranges.
func ScanSubnet(onFound func(Candidate)) ([]Candidate, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	var subnets []net.IP
	seen := make(map[string]bool)

	// Find all non-loopback IPv4 addresses and their subnets
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipv4 := ipnet.IP.To4(); ipv4 != nil {
				// Calculate subnet base (assuming /24)
				base := net.IPv4(ipv4[0], ipv4[1], ipv4[2], 0)
				if !seen[base.String()] {
					subnets = append(subnets, base)
					seen[base.String()] = true
					log.Printf("[scan] found local subnet: %s/24 (from %s)", base, ipv4)
				}
			}
		}
	}

	// Check for manual overrides via environment variable
	// Example: SCAN_SUBNETS="192.168.1.0/24,10.0.0.0/24"
	if env := os.Getenv("SCAN_SUBNETS"); env != "" {
		for _, s := range strings.Split(env, ",") {
			s = strings.TrimSpace(s)
			ip, _, err := net.ParseCIDR(s)
			if err != nil {
				// Try parsing as just an IP and assume /24
				ip = net.ParseIP(s)
				if ip == nil {
					log.Printf("[scan] invalid manual subnet: %s", s)
					continue
				}
			}
			ipv4 := ip.To4()
			if ipv4 == nil {
				continue
			}
			base := net.IPv4(ipv4[0], ipv4[1], ipv4[2], 0)
			if !seen[base.String()] {
				subnets = append(subnets, base)
				seen[base.String()] = true
				log.Printf("[scan] added manual subnet: %s/24", base)
			}
		}
	}

	if len(subnets) == 0 {
		return nil, fmt.Errorf("no local IP found")
	}

	candidates := []Candidate{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrency to avoid file descriptor exhaustion
	sem := make(chan struct{}, 100)

	// Initial ARP table
	arpTable := getARPTable()
	var arpMu sync.Mutex

	// Scan each subnet
	for _, baseIP := range subnets {
		log.Printf("[scan] scanning subnet %s/24...", baseIP)
		// Scan 1-254
		for i := 1; i < 255; i++ {
			// Reconstruct IP: baseIP is 16 bytes (IPv4-mapped), so bytes 12-15 are the IPv4 address
			ip := net.IPv4(baseIP[12], baseIP[13], baseIP[14], byte(i))

			wg.Add(1)
			go func(targetIP string) {
				defer wg.Done()
				sem <- struct{}{}        // Acquire
				defer func() { <-sem }() // Release

				address := fmt.Sprintf("%s:22", targetIP)
				// Increased timeout to 2s to catch slower VMs
				conn, err := net.DialTimeout("tcp", address, 2*time.Second)
				if err == nil {
					// Try to read SSH banner
					banner := ""
					conn.SetReadDeadline(time.Now().Add(1 * time.Second))
					buf := make([]byte, 256)
					n, _ := conn.Read(buf)
					if n > 0 {
						banner = strings.TrimSpace(string(buf[:n]))
					}
					conn.Close()

					// Construct candidate
					c := Candidate{IP: targetIP, Port: 22, Banner: banner}

					// Try to resolve MAC
					arpMu.Lock()
					mac, ok := arpTable[targetIP]
					if !ok {
						// Refresh ARP table if not found (maybe it just appeared)
						// This is a bit expensive but happens only on success
						arpTable = getARPTable()
						mac = arpTable[targetIP]
					}
					arpMu.Unlock()

					if mac != "" {
						c.MAC = mac
						if isTurtlebot(mac) {
							c.Manufacturer = "Raspberry Pi"
						}
					}

					// Fallback manufacturer check
					if c.Manufacturer == "" && c.Banner != "" {
						lowerBanner := strings.ToLower(c.Banner)
						if strings.Contains(lowerBanner, "raspbian") || strings.Contains(lowerBanner, "ubuntu") {
							c.Manufacturer = "Raspberry Pi"
						}
					}

					mu.Lock()
					candidates = append(candidates, c)
					mu.Unlock()
					log.Printf("[scan] found candidate: %s (banner: %q)", targetIP, banner)

					if onFound != nil {
						onFound(c)
					}
				}
			}(ip.String())
		}
	}

	wg.Wait()

	log.Printf("[scan] complete. found %d candidates", len(candidates))
	return candidates, nil
}
