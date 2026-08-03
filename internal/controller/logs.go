package controller

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"example.com/openrobot-fleet/internal/db"
)

// rosLogPath is where `ros2 launch` writes its aggregated console output.
// ROS 2 (Humble+) does not send node/launch output to the systemd journal by
// default even when run as a service, so we tail the log file directly
// instead of using journalctl.
const rosLogPath = "~/.ros/log/latest/launch.log"

// HandleLogs streams the live ROS 2 launch log from the robot to the browser
// over a websocket, read-only, so instructors can confirm ROS is up.
func (c *Controller) HandleLogs(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DEMO_MODE") == "true" {
		http.Error(w, "logs disabled in demo mode", http.StatusForbidden)
		return
	}

	id, err := parseRobotID(r.URL.Path)
	if err != nil {
		http.Error(w, "invalid robot id", http.StatusBadRequest)
		return
	}

	robot, err := c.DB.GetRobotByID(r.Context(), id)
	if err != nil {
		http.Error(w, "robot not found", http.StatusNotFound)
		return
	}

	if robot.InstallConfig == nil {
		robot.InstallConfig = &db.InstallConfig{}
	}

	if robot.InstallConfig.User == "" || robot.InstallConfig.SSHKey == "" {
		defaultCfg, err := c.DB.GetDefaultInstallConfig(r.Context())
		if err == nil && defaultCfg != nil {
			if robot.InstallConfig.User == "" {
				robot.InstallConfig.User = defaultCfg.User
			}
			if robot.InstallConfig.SSHKey == "" {
				robot.InstallConfig.SSHKey = defaultCfg.SSHKey
			}
		}
	}

	addr := robot.IP
	if addr == "" {
		addr = robot.InstallConfig.Address
	}

	if addr == "" || robot.InstallConfig.User == "" || robot.InstallConfig.SSHKey == "" {
		http.Error(w, "robot ssh credentials missing", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	defer ws.Close()

	signer, err := ssh.ParsePrivateKey([]byte(robot.InstallConfig.SSHKey))
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte("error: invalid private key\r\n"))
		return
	}

	config := &ssh.ClientConfig{
		User:            robot.InstallConfig.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	if !strings.Contains(addr, ":") {
		addr = addr + ":22"
	}

	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("error: ssh dial failed: %v\r\n", err)))
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("error: ssh session failed: %v\r\n", err)))
		return
	}
	defer session.Close()

	stdout, err := session.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return
	}

	cmd := fmt.Sprintf("tail -F -n 200 %s", rosLogPath)
	if err := session.Start(cmd); err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("error: failed to start tail: %v\r\n", err)))
		return
	}

	done := make(chan struct{})
	var once sync.Once
	pipe := func(r interface{ Read([]byte) (int, error) }) {
		buf := make([]byte, 1024)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		once.Do(func() { close(done) })
	}

	go pipe(stdout)
	go pipe(stderr)

	// Drain incoming messages so a browser close is detected promptly; the
	// stream is read-only, so any client data is simply ignored.
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				session.Signal(ssh.SIGTERM)
				session.Close()
				return
			}
		}
	}()

	<-done
}
