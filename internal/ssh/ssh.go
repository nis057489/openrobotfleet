package sshc

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

type HostSpec struct {
	Addr         string
	User         string
	PrivateKey   []byte
	Password     string
	UseSudo      bool
	SudoPassword string
}

// InstallAgent uploads the agent binary/config/service and enables the unit remotely.
func InstallAgent(h HostSpec, cfg agent.Config, agentBinary []byte) error {
	if h.Addr == "" || h.User == "" {
		return fmt.Errorf("host addr and user required")
	}

	var authMethods []ssh.AuthMethod
	if len(h.PrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(bytes.TrimSpace(h.PrivateKey))
		if err != nil {
			return fmt.Errorf("parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if h.Password != "" {
		authMethods = append(authMethods, ssh.Password(h.Password))
	}
	if len(authMethods) == 0 {
		return fmt.Errorf("no auth methods provided")
	}

	sshConfig := &ssh.ClientConfig{
		User:            h.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", h.Addr, sshConfig)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", h.Addr, err)
	}
	defer client.Close()

	// If we have a private key, try to install it to authorized_keys
	if len(h.PrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(bytes.TrimSpace(h.PrivateKey))
		if err == nil {
			pubKey := ssh.MarshalAuthorizedKey(signer.PublicKey())
			// Ensure .ssh directory exists and append key
			cmd := fmt.Sprintf("mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '%s' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys", strings.TrimSpace(string(pubKey)))
			if err := runRemote(client, cmd, "", false); err != nil {
				log.Printf("warning: failed to install ssh key: %v", err)
			} else {
				log.Printf("installed ssh key on %s", h.Addr)
			}
		}
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()

	cfgBytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	type remoteFile struct {
		tmp  string
		dst  string
		mode os.FileMode
		data []byte
	}
	files := []remoteFile{
		{dst: "/usr/local/bin/openrobot-agent", mode: 0o755, data: agentBinary},
		{dst: "/etc/openrobot-agent/config.yaml", mode: 0o644, data: cfgBytes},
		{dst: "/etc/systemd/system/openrobot-agent.service", mode: 0o644, data: []byte(systemdUnit)},
	}

	if h.UseSudo {
		for i := range files {
			files[i].tmp = fmt.Sprintf("/tmp/openrobot-agent-%d-%d", time.Now().UnixNano(), i)
			if err := writeRemoteFile(sftpClient, files[i].tmp, files[i].data, 0o600); err != nil {
				return err
			}
		}
	} else {
		for _, file := range files {
			if err := sftpClient.MkdirAll(filepath.Dir(file.dst)); err != nil {
				return fmt.Errorf("mkdir %s: %w", filepath.Dir(file.dst), err)
			}
			if err := writeRemoteFile(sftpClient, file.dst, file.data, file.mode); err != nil {
				return err
			}
		}
	}

	commands := []string{"set -e"}
	if h.UseSudo {
		for _, file := range files {
			mode := fmt.Sprintf("%04o", file.mode.Perm())
			commands = append(commands,
				fmt.Sprintf("install -D -m %s %s %s", mode, file.tmp, file.dst),
				fmt.Sprintf("rm -f %s", file.tmp))
		}
	}
	commands = append(commands,
		"mkdir -p /home/ubuntu/.ros",
		"chown -R ubuntu:ubuntu /home/ubuntu/.ros",
		"systemctl daemon-reload",
		"systemctl enable openrobot-agent",
		"systemctl restart openrobot-agent",
	)
	script := strings.Join(commands, " && ")
	if err := runRemote(client, script, h.SudoPassword, h.UseSudo); err != nil {
		return fmt.Errorf("run remote command: %w", err)
	}
	log.Printf("installed openrobot-agent on %s", h.Addr)
	return nil
}

func writeRemoteFile(c *sftp.Client, path string, data []byte, perm os.FileMode) error {
	f, err := c.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("open remote file %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write remote file %s: %w", path, err)
	}
	if err := c.Chmod(path, perm); err != nil {
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	return nil
}

func runRemote(client *ssh.Client, script, sudoPassword string, useSudo bool) error {
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()
	var output bytes.Buffer
	sess.Stdout = &output
	sess.Stderr = &output
	cmd := fmt.Sprintf("bash -lc %q", script)
	var stdin io.WriteCloser
	if useSudo {
		if sudoPassword == "" {
			return fmt.Errorf("sudo password required")
		}
		var err error
		stdin, err = sess.StdinPipe()
		if err != nil {
			return fmt.Errorf("stdin pipe: %w", err)
		}
		cmd = fmt.Sprintf("sudo -S -p '' %s", cmd)
		go func() {
			defer stdin.Close()
			io.WriteString(stdin, sudoPassword+"\n")
		}()
	}
	if err := sess.Run(cmd); err != nil {
		return fmt.Errorf("command failed: %w (output: %s)", err, output.String())
	}
	return nil
}

const systemdUnit = `[Unit]
Description=OpenRobot Agent
After=network-online.target

[Service]
ExecStart=/usr/local/bin/openrobot-agent --config /etc/openrobot-agent/config.yaml
Restart=always

[Install]
WantedBy=multi-user.target
`

// DetectArch connects to the host and returns the architecture (amd64, arm64).
func DetectArch(h HostSpec) (string, error) {
	if h.Addr == "" || h.User == "" {
		return "", fmt.Errorf("host addr and user required")
	}

	var authMethods []ssh.AuthMethod
	if len(h.PrivateKey) > 0 {
		signer, err := ssh.ParsePrivateKey(bytes.TrimSpace(h.PrivateKey))
		if err != nil {
			return "", fmt.Errorf("parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if h.Password != "" {
		authMethods = append(authMethods, ssh.Password(h.Password))
	}
	if len(authMethods) == 0 {
		return "", fmt.Errorf("no auth methods provided")
	}

	sshConfig := &ssh.ClientConfig{
		User:            h.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", h.Addr, sshConfig)
	if err != nil {
		return "", fmt.Errorf("ssh dial %s: %w", h.Addr, err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	out, err := session.Output("uname -m")
	if err != nil {
		return "", fmt.Errorf("uname -m: %w", err)
	}
	arch := strings.TrimSpace(string(out))
	switch arch {
	case "x86_64":
		return "amd64", nil
	case "aarch64", "arm64":
		return "arm64", nil
	default:
		return arch, nil
	}
}
