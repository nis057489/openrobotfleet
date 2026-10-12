// Command https-timesync feeds chronyd time samples measured over HTTPS, for
// networks that block NTP (udp/123). It runs on the lab router: each sample
// goes to a chrony SOCK refclock, chronyd filters them and disciplines the
// clock (offset and drift, without steps), and the LAN syncs to chronyd over
// ordinary NTP. The chrony.conf line it expects:
//
//	refclock SOCK /var/run/chrony.https.sock refid HTTP poll 6 delay 0.01
//
// Build for the router with:
//
//	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build ./cmd/https-timesync
package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// sockMagic identifies a sample to chrony's SOCK refclock ("SOCK").
const sockMagic = 0x534f434b

// sockSample mirrors chrony's struct sock_sample on 64-bit Linux.
type sockSample struct {
	Sec    int64 // local time of the measurement
	Usec   int64
	Offset float64 // reference time minus local time, in seconds
	Pulse  int32
	Leap   int32
	Pad    int32
	Magic  int32
}

func main() {
	url := flag.String("url", "https://time.akamai.com/?ms", "endpoint returning unix time as seconds with a fractional part")
	sock := flag.String("sock", "/var/run/chrony.https.sock", "chrony SOCK refclock path")
	interval := flag.Duration("interval", 16*time.Second, "time between samples")
	burst := flag.Int("burst", 4, "requests per sample; the one with the shortest round trip is used")
	once := flag.Bool("once", false, "print one measurement and exit")
	flag.Parse()
	log.SetFlags(0)

	secure := newClient(false)
	// The router has no RTC, so after a reboot its clock can be far enough
	// off that certificates look expired. Only for that case, measure
	// without verification so chronyd can correct the clock.
	insecure := newClient(true)

	for {
		at, offset, rtt, err := measure(secure, *url, *burst)
		var certErr x509.CertificateInvalidError
		if errors.As(err, &certErr) && certErr.Reason == x509.Expired {
			at, offset, rtt, err = measure(insecure, *url, *burst)
		}
		switch {
		case err != nil:
			log.Printf("measure: %v", err)
		case *once:
			fmt.Printf("offset %+.4fs rtt %.4fs\n", offset, rtt.Seconds())
			return
		default:
			if err := send(*sock, at, offset); err != nil {
				log.Printf("send to chronyd: %v", err)
			}
		}
		if *once {
			return
		}
		time.Sleep(*interval)
	}
}

func newClient(skipVerify bool) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: skipVerify},
			MaxIdleConnsPerHost: 1,
			IdleConnTimeout:     time.Minute,
		},
	}
}

// measure makes burst requests over one kept-alive connection (plus a first
// one to open it) and returns the sample with the shortest round trip. The
// server's time is assumed to be read at the midpoint of the request.
func measure(client *http.Client, url string, burst int) (at time.Time, offset float64, rtt time.Duration, err error) {
	rtt = -1
	for i := 0; i <= burst; i++ {
		t0 := time.Now()
		server, reqErr := fetch(client, url)
		t1 := time.Now()
		if reqErr != nil {
			return at, 0, 0, reqErr
		}
		if i == 0 {
			continue // connection setup inflates the first round trip
		}
		d := t1.Sub(t0)
		if rtt < 0 || d < rtt {
			rtt = d
			at = t0.Add(d / 2)
			offset = server - float64(at.UnixNano())/1e9
		}
	}
	return at, offset, rtt, nil
}

func fetch(client *http.Client, url string) (float64, error) {
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s: %s", url, resp.Status)
	}
	text := strings.TrimSpace(string(body))
	if !strings.Contains(text, ".") {
		return 0, fmt.Errorf("%s: no sub-second precision in %q", url, text)
	}
	return strconv.ParseFloat(text, 64)
}

func send(path string, at time.Time, offset float64) error {
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	return binary.Write(conn, binary.NativeEndian, sockSample{
		Sec:    at.Unix(),
		Usec:   int64(at.Nanosecond() / 1000),
		Offset: offset,
		Magic:  sockMagic,
	})
}
