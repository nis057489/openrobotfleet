package controller

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type terminalMessage struct {
	Type string `json:"type"` // "data" or "resize"
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func (c *Controller) HandleTerminal(w http.ResponseWriter, r *http.Request) {
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

	if robot.InstallConfig == nil || robot.InstallConfig.Address == "" || robot.InstallConfig.User == "" || robot.InstallConfig.SSHKey == "" {
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

	addr := robot.InstallConfig.Address
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

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm", 40, 80, modes); err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("error: pty request failed: %v\r\n", err)))
		return
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return
	}

	if err := session.Shell(); err != nil {
		ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("error: shell failed: %v\r\n", err)))
		return
	}

	// Pipe stdout/stderr to websocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()
	
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if err != nil {
				return
			}
			if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// Read from websocket and write to stdin
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			break
		}

		var tm terminalMessage
		if json.Unmarshal(msg, &tm) == nil {
			if tm.Type == "resize" {
				session.WindowChange(tm.Rows, tm.Cols)
				continue
			}
			if tm.Type == "data" {
				stdin.Write([]byte(tm.Data))
				continue
			}
		}
		
		// Fallback: just write to stdin if not JSON
		stdin.Write(msg)
	}
}

func parseRobotID(path string) (int64, error) {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "robots" && i+1 < len(parts) {
			return strconv.ParseInt(parts[i+1], 10, 64)
		}
	}
	return 0, fmt.Errorf("robot id not found in path")
}
