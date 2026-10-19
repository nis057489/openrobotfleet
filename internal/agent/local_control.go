package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// localControlSocket lets programs in the desktop session, such as the
// laptop's clock indicator, ask the agent (which runs as root) for a small
// fixed set of actions, since students may not have sudo. Anyone on the
// device may use it, so it only offers actions that are safe to repeat.
const localControlSocket = "/run/openrobotfleet-agent.sock"

// requestQueue holds requests for the controller, sent with the next
// heartbeat: agents can only publish their own status topic.
type requestQueue struct {
	mu      sync.Mutex
	pending []string
}

func (q *requestQueue) add(r string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, p := range q.pending {
		if p == r {
			return
		}
	}
	q.pending = append(q.pending, r)
}

func (q *requestQueue) take() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	p := q.pending
	q.pending = nil
	return p
}

func (q *requestQueue) key() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return strings.Join(q.pending, ",")
}

func (e *AgentEngine) serveLocalControl(ctx context.Context) {
	os.Remove(localControlSocket)
	l, err := net.Listen("unix", localControlSocket)
	if err != nil {
		log.Printf("[agent] local control socket: %v", err)
		return
	}
	if err := os.Chmod(localControlSocket, 0o666); err != nil {
		log.Printf("[agent] local control socket: %v", err)
	}
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go e.handleLocalControl(conn)
	}
}

// handleLocalControl serves one request line: "resync-time" re-syncs this
// device's clock now; "clean-laptop" stops the laptop's ROS processes (see
// stopLaptopROS) and then re-syncs its clock; "fix-robot" asks the
// controller to resync the clock of this laptop's group robot and restart
// its ROS.
func (e *AgentEngine) handleLocalControl(conn net.Conn) {
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(time.Minute))
	line, _ := bufio.NewReader(io.LimitReader(conn, 64)).ReadString('\n')
	switch req := strings.TrimSpace(line); req {
	case "resync-time":
		if err := HandleResyncTime(); err != nil {
			fmt.Fprintf(conn, "error: %v\n", err)
			return
		}
		fmt.Fprintln(conn, "ok")
	case "clean-laptop":
		if err := stopLaptopROS(e.Config); err != nil {
			fmt.Fprintf(conn, "error: %v\n", err)
			return
		}
		if err := HandleResyncTime(); err != nil {
			fmt.Fprintf(conn, "error: clock didn't sync: %v\n", err)
			return
		}
		fmt.Fprintln(conn, "ok")
	case "fix-robot":
		e.requests.add("fix_robot")
		fmt.Fprintln(conn, "ok")
	default:
		fmt.Fprintf(conn, "error: unknown request %q\n", req)
	}
}

// HandleResyncTime makes the NTP client sync now rather than at its next
// poll, stepping the clock if it is off.
func HandleResyncTime() error {
	if chronyActive() {
		runCmd(defaultCmdTimeout, "chronyc", "burst", "4/4")
		time.Sleep(5 * time.Second)
		if out, err := runCmd(defaultCmdTimeout, "chronyc", "makestep"); err != nil {
			return fmt.Errorf("chronyc makestep: %v: %s", err, strings.TrimSpace(string(out)))
		}
	} else if out, err := runCmd(defaultCmdTimeout, "systemctl", "restart", "systemd-timesyncd"); err != nil {
		return fmt.Errorf("restart systemd-timesyncd: %v: %s", err, strings.TrimSpace(string(out)))
	}
	// timesyncd queries the server as soon as it starts.
	for i := 0; i < 20; i++ {
		time.Sleep(time.Second)
		if ClockSynchronized() {
			log.Printf("[agent] clock resynced")
			return nil
		}
	}
	return errors.New("clock not synchronised 20 s after resyncing; is the lab time server reachable?")
}
