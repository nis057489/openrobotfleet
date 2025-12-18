package controller

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/crypto/ssh"

	"example.com/openrobot-fleet/internal/db"
)

func (c *Controller) GetGoldenImageConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := c.DB.GetGoldenImageConfig(r.Context())
	if err != nil {
		log.Printf("get golden image config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load config")
		return
	}
	respondJSON(w, http.StatusOK, map[string]*db.GoldenImageConfig{"config": cfg})
}

func (c *Controller) SaveGoldenImageConfig(w http.ResponseWriter, r *http.Request) {
	var req db.GoldenImageConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid config")
		return
	}
	if err := c.DB.SaveGoldenImageConfig(r.Context(), req); err != nil {
		log.Printf("save golden image config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save config")
		return
	}
	respondJSON(w, http.StatusOK, map[string]*db.GoldenImageConfig{"config": &req})
}

func (c *Controller) DownloadGoldenImage(w http.ResponseWriter, r *http.Request) {
	cfg, err := c.DB.GetGoldenImageConfig(r.Context())
	if err != nil {
		log.Printf("get golden image config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load config")
		return
	}
	if cfg == nil {
		respondError(w, http.StatusBadRequest, "golden image config not set")
		return
	}

	// Fetch default install config for SSH key
	installCfg, err := c.DB.GetDefaultInstallConfig(r.Context())
	sshKey := ""
	if err == nil && installCfg != nil {
		sshKey = installCfg.SSHKey
	}

	pubKey, _ := prepareSSHKeys(sshKey)

	tmplData := struct {
		*db.GoldenImageConfig
		SSHPublicKey string
	}{
		GoldenImageConfig: cfg,
		SSHPublicKey:      pubKey,
	}

	w.Header().Set("Content-Type", "text/yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=user-data")

	tmpl, err := template.New("user-data").Parse(userDataTemplate)
	if err != nil {
		log.Printf("parse template: %v", err)
		respondError(w, http.StatusInternalServerError, "template error")
		return
	}

	if err := tmpl.Execute(w, tmplData); err != nil {
		log.Printf("execute template: %v", err)
	}
}

const userDataTemplate = `#cloud-config
hostname: openrobot
manage_etc_hosts: true
users:
  - name: ubuntu
    groups: [sudo, dialout, video]
    shell: /bin/bash
    sudo: ['ALL=(ALL) NOPASSWD:ALL']
    lock_passwd: false
    passwd: $6$rounds=4096$randomsalt$encryptedpassword
    ssh_authorized_keys:
      {{if .SSHPublicKey}}- {{.SSHPublicKey}}{{end}}

# Packages are pre-installed in the golden image.
# We only handle runtime configuration here.

write_files:
  - path: /etc/netplan/50-cloud-init.yaml
    content: |
      network:
        version: 2
        ethernets:
          eth0:
            dhcp4: true
            optional: true
        wifis:
          wlan0:
            dhcp4: true
            optional: true
            access-points:
              "{{.WifiSSID}}":
                password: "{{.WifiPassword}}"

  - path: /etc/apt/apt.conf.d/20auto-upgrades
    content: |
      APT::Periodic::Update-Package-Lists "0";
      APT::Periodic::Unattended-Upgrade "0";

  - path: /etc/openrobotfleet-agent/config.yaml
    content: |
      agent_id: "ROBOT-UNINITIALIZED"
      mqtt_broker: "{{.MQTTBroker}}"
      workspace_path: "/home/ubuntu/ros_ws/src"

runcmd:
  # Generate unique Agent ID and Hostname
  - |
    SUFFIX=$(head /dev/urandom | tr -dc a-z0-9 | head -c 6)
    sed -i "s/ROBOT-UNINITIALIZED/robot-$SUFFIX/" /etc/openrobotfleet-agent/config.yaml
    hostnamectl set-hostname robot-$SUFFIX
    sed -i "s/openrobot/robot-$SUFFIX/g" /etc/hosts

  # Fix DNS (Docker/Systemd conflict)
  - rm -f /etc/resolv.conf
  - ln -s /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf
  - systemctl restart systemd-resolved

  # Network setup
  - netplan apply
  - systemctl mask systemd-networkd-wait-online.service

  # Environment variables
  {{if eq .RobotModel "TB4"}}
  - echo 'export ROS_DOMAIN_ID={{.ROSDomainID}}' >> /home/ubuntu/.bashrc
  # TB4 setup script handles other env vars
  {{else}}
  # TB3 Default
  - echo 'source /opt/ros/{{if eq .ROSVersion "Jazzy"}}jazzy{{else}}humble{{end}}/setup.bash' >> /home/ubuntu/.bashrc
  - echo 'source /home/ubuntu/ros_ws/install/setup.bash' >> /home/ubuntu/.bashrc
  - echo 'export ROS_DOMAIN_ID={{.ROSDomainID}}' >> /home/ubuntu/.bashrc
  - echo 'export LDS_MODEL={{.LDSModel}}' >> /home/ubuntu/.bashrc
  {{end}}

  # Fix ROS permissions
  - mkdir -p /home/ubuntu/.ros
  - chown -R ubuntu:ubuntu /home/ubuntu/.ros

  # Agent Service (Binary is pre-installed)
  - |
    cat <<EOF > /etc/systemd/system/openrobotfleet-agent.service
    [Unit]
    Description=OpenRobot Agent
    After=network.target

    [Service]
    ExecStart=/usr/local/bin/openrobotfleet-agent
    Restart=always
    User=root
    Environment=AGENT_CONFIG_PATH=/etc/openrobotfleet-agent/config.yaml

    [Install]
    WantedBy=multi-user.target
    EOF
  - systemctl enable openrobotfleet-agent
  - systemctl start openrobotfleet-agent

final_message: "OpenRobot setup complete. Ready to roll!"
`

var (
	buildLock      sync.Mutex
	buildStatus    = "idle" // idle, building, success, error
	buildError     string
	buildProgress  int      // 0-100
	buildStep      string   // Current step description
	buildLogs      []string // New
	buildImageName string   // New
	lastLogUpdate  time.Time
)

func (c *Controller) logBuild(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Print(msg)
	buildLock.Lock()
	// Prepend timestamp
	ts := time.Now().Format("15:04:05")
	buildLogs = append(buildLogs, fmt.Sprintf("[%s] %s", ts, msg))
	// Limit log size
	if len(buildLogs) > 2000 {
		buildLogs = buildLogs[len(buildLogs)-2000:]
	}

	// Throttle updates to frontend to avoid flooding
	shouldUpdate := time.Since(lastLogUpdate) > 200*time.Millisecond
	if shouldUpdate {
		lastLogUpdate = time.Now()
	}

	// Capture state for callback
	status := buildStatus
	logs := make([]string, len(buildLogs))
	copy(logs, buildLogs)
	progress := buildProgress
	step := buildStep
	err := buildError
	imageName := buildImageName
	buildLock.Unlock()

	if shouldUpdate && c.OnBuildUpdate != nil {
		c.OnBuildUpdate(status, progress, step, logs, err, imageName)
	}
}

func (c *Controller) BuildGoldenImage(w http.ResponseWriter, r *http.Request) {
	if os.Getenv("DEMO_MODE") == "true" {
		respondError(w, http.StatusForbidden, "Build feature is disabled in demo mode")
		return
	}
	buildLock.Lock()
	if buildStatus == "building" {
		buildLock.Unlock()
		respondError(w, http.StatusConflict, "build already in progress")
		return
	}
	buildStatus = "building"
	buildError = ""
	buildProgress = 0
	buildStep = "Starting build..."
	buildLogs = []string{}
	buildImageName = ""
	buildLock.Unlock()

	go c.runBuild()

	respondJSON(w, http.StatusAccepted, map[string]string{"status": "started"})
}

func (c *Controller) GetBuildStatus(w http.ResponseWriter, r *http.Request) {
	buildLock.Lock()
	defer buildLock.Unlock()
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"status":     buildStatus,
		"error":      buildError,
		"progress":   buildProgress,
		"step":       buildStep,
		"logs":       buildLogs,
		"image_name": buildImageName,
	})
}

func (c *Controller) updateBuildProgress(step string, progress int) {
	buildLock.Lock()
	buildStep = step
	buildProgress = progress
	// Also log the step
	ts := time.Now().Format("15:04:05")
	buildLogs = append(buildLogs, fmt.Sprintf("[%s] >>> %s", ts, step))

	// Capture state for callback
	status := buildStatus
	logs := make([]string, len(buildLogs))
	copy(logs, buildLogs)
	err := buildError
	imageName := buildImageName
	buildLock.Unlock()

	if c.OnBuildUpdate != nil {
		c.OnBuildUpdate(status, progress, step, logs, err, imageName)
	}
}

func (c *Controller) runBuild() {
	defer func() {
		if r := recover(); r != nil {
			c.failBuild(fmt.Sprintf("panic: %v", r))
		}
	}()

	// 1. Load Config
	c.updateBuildProgress("Loading configuration...", 5)
	ctx := context.Background()
	cfg, err := c.DB.GetGoldenImageConfig(ctx)
	if err != nil || cfg == nil {
		c.failBuild("failed to load config")
		return
	}
	c.logBuild("Config loaded: RobotModel=%s, ROSVersion=%s", cfg.RobotModel, cfg.ROSVersion)

	// 2. Prepare directories
	c.updateBuildProgress("Preparing directories...", 10)
	webRoot := os.Getenv("WEB_ROOT")
	if webRoot == "" {
		webRoot = "./web/dist"
	}
	imagesDir := filepath.Join(webRoot, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		c.failBuild(fmt.Sprintf("mkdir failed: %v", err))
		return
	}

	// 3. Download Base Image
	c.updateBuildProgress("Downloading base image (this may take a while)...", 15)

	// Determine Image URL based on ROS Version
	baseImageURL := "https://cdimage.ubuntu.com/releases/22.04/release/ubuntu-22.04.5-preinstalled-server-arm64+raspi.img.xz"
	baseImageName := "ubuntu-22.04-server-arm64.img.xz"

	if cfg.ROSVersion == "Jazzy" {
		baseImageURL = "https://cdimage.ubuntu.com/releases/24.04/release/ubuntu-24.04.3-preinstalled-server-arm64+raspi.img.xz"
		baseImageName = "ubuntu-24.04-server-arm64.img.xz"
	}

	// Fetch hash dynamically
	c.logBuild("fetching upstream hash for verification...")
	expectedSHA256, err := fetchRemoteHash(baseImageURL)
	if err != nil {
		c.failBuild(fmt.Sprintf("failed to fetch upstream hash: %v", err))
		return
	}
	c.logBuild("upstream hash: %s", expectedSHA256)

	// Cache it in /data/image-cache (persistent volume) if available, else /tmp
	cacheDir := "/tmp/image-cache"
	if _, err := os.Stat("/data"); err == nil {
		cacheDir = "/data/image-cache"
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		c.failBuild(fmt.Sprintf("cache dir failed: %v", err))
		return
	}
	baseImageXZ := filepath.Join(cacheDir, baseImageName)

	// Check if file exists and verify hash
	downloadNeeded := true
	if _, err := os.Stat(baseImageXZ); err == nil {
		c.logBuild("verifying existing image hash...")
		if verifyHash(baseImageXZ, expectedSHA256) {
			c.logBuild("hash verified, skipping download")
			downloadNeeded = false
		} else {
			c.logBuild("hash mismatch, re-downloading...")
			os.Remove(baseImageXZ)
		}
	}

	if downloadNeeded {
		c.logBuild("downloading base image from %s...", baseImageURL)
		cmd := exec.Command("wget", "-O", baseImageXZ, baseImageURL)
		if out, err := cmd.CombinedOutput(); err != nil {
			c.failBuild(fmt.Sprintf("download failed: %v: %s", err, string(out)))
			return
		}
		// Verify after download
		if !verifyHash(baseImageXZ, expectedSHA256) {
			c.failBuild("downloaded file hash mismatch")
			os.Remove(baseImageXZ)
			return
		}
	}

	// 4. Decompress to working copy
	c.updateBuildProgress("Decompressing image...", 25)

	// Construct image name
	robotModel := cfg.RobotModel
	if robotModel == "" {
		robotModel = "TB3"
	}
	rosVersion := cfg.ROSVersion
	if rosVersion == "" {
		rosVersion = "Humble"
	}
	imageName := fmt.Sprintf("turtlebot-%s-%s-golden.img", strings.ToLower(robotModel), strings.ToLower(rosVersion))
	workImage := filepath.Join(imagesDir, imageName)

	c.logBuild("decompressing to %s...", workImage)
	cmd := exec.Command("xz", "-d", "-k", "-c", baseImageXZ)
	outFile, err := os.Create(workImage)
	if err != nil {
		c.failBuild(fmt.Sprintf("create work image failed: %v", err))
		return
	}
	cmd.Stdout = outFile
	if err := cmd.Run(); err != nil {
		outFile.Close()
		c.failBuild(fmt.Sprintf("decompress failed: %v", err))
		return
	}
	outFile.Close()

	// 5. Expand Image (+4GB)
	c.updateBuildProgress("Expanding image...", 35)
	c.logBuild("expanding image by 4GB...")
	if err := exec.Command("truncate", "-s", "+4G", workImage).Run(); err != nil {
		c.failBuild(fmt.Sprintf("truncate failed: %v", err))
		return
	}

	// 6. Setup Loop Device
	c.updateBuildProgress("Setting up loop device...", 40)
	c.logBuild("setting up loop device...")

	if err := ensureLoopDevices(); err != nil {
		c.logBuild("warning: failed to ensure loop devices: %v", err)
	}

	out, err := exec.Command("losetup", "-fP", "--show", workImage).CombinedOutput()
	if err != nil {
		c.failBuild(fmt.Sprintf("losetup failed: %v: %s", err, string(out)))
		return
	}
	loopDev := strings.TrimSpace(string(out))
	defer exec.Command("losetup", "-d", loopDev).Run()

	// 7. Resize Partition and Filesystem
	c.updateBuildProgress("Resizing partitions...", 45)
	c.logBuild("resizing partition 2 on %s...", loopDev)
	if out, err := exec.Command("parted", "-s", loopDev, "resizepart", "2", "100%").CombinedOutput(); err != nil {
		c.failBuild(fmt.Sprintf("parted failed: %v: %s", err, string(out)))
		return
	}

	// Force kernel to re-read partition table
	exec.Command("partprobe", loopDev).Run()
	time.Sleep(2 * time.Second)

	// Ensure device nodes exist (Docker container might not have udev)
	if err := ensureDeviceNode(loopDev + "p1"); err != nil {
		c.logBuild("warning: ensureDeviceNode p1: %v", err)
	}
	if err := ensureDeviceNode(loopDev + "p2"); err != nil {
		c.logBuild("warning: ensureDeviceNode p2: %v", err)
	}

	c.logBuild("resizing filesystem on %sp2...", loopDev)
	if out, err := exec.Command("resize2fs", loopDev+"p2").CombinedOutput(); err != nil {
		c.failBuild(fmt.Sprintf("resize2fs failed: %v: %s", err, string(out)))
		return
	}

	// 8. Mount
	c.updateBuildProgress("Mounting image...", 50)
	mntDir := "/mnt/turtlebot-build"
	os.MkdirAll(mntDir, 0755)
	defer os.RemoveAll(mntDir)

	// Mount root
	if out, err := exec.Command("mount", loopDev+"p2", mntDir).CombinedOutput(); err != nil {
		c.failBuild(fmt.Sprintf("mount root failed: %v: %s", err, string(out)))
		return
	}
	defer exec.Command("umount", "-R", mntDir).Run()

	// Mount boot (firmware)
	os.MkdirAll(filepath.Join(mntDir, "boot/firmware"), 0755)
	if out, err := exec.Command("mount", loopDev+"p1", filepath.Join(mntDir, "boot/firmware")).CombinedOutput(); err != nil {
		c.failBuild(fmt.Sprintf("mount boot failed: %v: %s", err, string(out)))
		return
	}

	// 9. Prepare Chroot
	c.updateBuildProgress("Preparing chroot environment...", 55)
	c.logBuild("preparing chroot...")
	// Copy qemu-aarch64-static
	if out, err := exec.Command("cp", "/usr/bin/qemu-aarch64-static", filepath.Join(mntDir, "usr/bin/")).CombinedOutput(); err != nil {
		c.failBuild(fmt.Sprintf("cp qemu failed: %v: %s", err, string(out)))
		return
	}
	// Bind mounts
	for _, d := range []string{"proc", "sys", "dev", "dev/pts"} {
		if err := exec.Command("mount", "--bind", "/"+d, filepath.Join(mntDir, d)).Run(); err != nil {
			// dev/pts might fail if not present, ignore
			if d != "dev/pts" {
				c.failBuild(fmt.Sprintf("mount bind %s failed: %v", d, err))
				return
			}
		}
	}
	// DNS
	destResolv := filepath.Join(mntDir, "etc/resolv.conf")
	os.Remove(destResolv) // Remove existing file/symlink to avoid issues
	if err := exec.Command("cp", "/etc/resolv.conf", destResolv).Run(); err != nil {
		c.failBuild(fmt.Sprintf("cp resolv.conf failed: %v", err))
		return
	}

	// 10. Install ROS 2 & Agent
	c.updateBuildProgress("Installing ROS 2 and Agent (this takes 20-30 mins)...", 60)
	c.logBuild("installing ROS 2 and Agent (this may take a while)...")

	var installScript string
	if cfg.RobotModel == "TB4" {
		// TB4 Logic
		branch := "humble"
		if cfg.ROSVersion == "Jazzy" {
			branch = "jazzy"
		}
		installScript = fmt.Sprintf(`#!/bin/bash
set -e
export DEBIAN_FRONTEND=noninteractive

# Define sudo as a no-op since we are root
function sudo() { "$@"; }
export -f sudo

# Install prerequisites
apt-get update
apt-get install -y wget curl git

# Run official setup script
wget -qO - https://raw.githubusercontent.com/turtlebot/turtlebot4_setup/%s/scripts/turtlebot4_setup.sh | bash

# Cleanup
apt-get clean
rm -rf /var/lib/apt/lists/*
`, branch)
	} else {
		// TB3 Logic (Existing)
		installScript = `#!/bin/bash
set -e
export DEBIAN_FRONTEND=noninteractive

# Install ROS 2 Humble
apt-get update
apt-get install -y software-properties-common curl gnupg lsb-release
curl -sSL https://raw.githubusercontent.com/ros/rosdistro/master/ros.key -o /usr/share/keyrings/ros-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/ros-archive-keyring.gpg] http://packages.ros.org/ros2/ubuntu $(source /etc/os-release && echo $UBUNTU_CODENAME) main" | tee /etc/apt/sources.list.d/ros2.list > /dev/null
apt-get update
apt-get install -y ros-humble-ros-base ros-humble-turtlebot3-msgs ros-humble-dynamixel-sdk ros-humble-xacro ros-humble-hls-lfcd-lds-driver libudev-dev build-essential git python3-colcon-common-extensions

# Setup Workspace
mkdir -p /home/ubuntu/turtlebot3_ws/src
cd /home/ubuntu/turtlebot3_ws/src
git clone -b humble https://github.com/ROBOTIS-GIT/turtlebot3.git
git clone -b humble https://github.com/ROBOTIS-GIT/ld08_driver.git
cd /home/ubuntu/turtlebot3_ws
source /opt/ros/humble/setup.bash
colcon build --symlink-install --parallel-workers 1
chown -R 1000:1000 /home/ubuntu/turtlebot3_ws

# Udev Rules
cp /home/ubuntu/turtlebot3_ws/src/turtlebot3/turtlebot3_bringup/script/99-turtlebot3-cdc.rules /etc/udev/rules.d/

# Cleanup
apt-get clean
rm -rf /var/lib/apt/lists/*
`
	}
	if err := os.WriteFile(filepath.Join(mntDir, "tmp/install.sh"), []byte(installScript), 0755); err != nil {
		c.failBuild(fmt.Sprintf("write install script failed: %v", err))
		return
	}

	// Copy Agent Binary (assuming it's in current dir or path)
	// We are running in /app, agent binary is ./agent (from Dockerfile)
	// Golden images are always ARM64 (Raspberry Pi)
	binaryName := "agent-arm64"
	binaryPath := filepath.Join("/app", binaryName)
	if _, err := os.Stat(binaryPath); os.IsNotExist(err) {
		// Fallback to local dir if running locally
		binaryPath = "./" + binaryName
	}

	if out, err := exec.Command("cp", binaryPath, filepath.Join(mntDir, "usr/local/bin/openrobotfleet-agent")).CombinedOutput(); err != nil {
		c.logBuild("warning: could not copy agent binary: %v %s", err, string(out))
	}
	exec.Command("chmod", "+x", filepath.Join(mntDir, "usr/local/bin/openrobotfleet-agent")).Run()

	// Run Script in Chroot
	cmd = exec.Command("chroot", mntDir, "/bin/bash", "/tmp/install.sh")

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		c.failBuild(fmt.Sprintf("install script start failed: %v", err))
		return
	}

	// Stream logs
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			c.logBuild("[install] %s", scanner.Text())
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			c.logBuild("[install/err] %s", scanner.Text())
		}
	}()

	wg.Wait()

	if err := cmd.Wait(); err != nil {
		c.failBuild(fmt.Sprintf("install script failed: %v", err))
		return
	}

	// 11. Write User Data (Cloud Init)
	c.updateBuildProgress("Injecting configuration...", 90)
	c.logBuild("writing user-data...")
	userDataPath := filepath.Join(mntDir, "boot/firmware/user-data") // Ubuntu 22.04 Pi

	// Fetch default install config for SSH key
	installCfg, err := c.DB.GetDefaultInstallConfig(ctx)
	sshKey := ""
	if err == nil && installCfg != nil {
		sshKey = installCfg.SSHKey
	}

	pubKey, _ := prepareSSHKeys(sshKey)

	tmplData := struct {
		*db.GoldenImageConfig
		SSHPublicKey string
	}{
		GoldenImageConfig: cfg,
		SSHPublicKey:      pubKey,
	}

	tmpl, err := template.New("user-data").Parse(userDataTemplate)
	if err != nil {
		c.failBuild(fmt.Sprintf("template parse failed: %v", err))
		return
	}
	f, err := os.Create(userDataPath)
	if err != nil {
		c.failBuild(fmt.Sprintf("create user-data failed: %v", err))
		return
	}
	if err := tmpl.Execute(f, tmplData); err != nil {
		f.Close()
		c.failBuild(fmt.Sprintf("template execute failed: %v", err))
		return
	}
	f.Close()

	// Success
	buildLock.Lock()
	buildStatus = "success"
	buildProgress = 100
	buildStep = fmt.Sprintf("Build complete! Image: %s", imageName)
	buildImageName = imageName

	// Capture state
	logs := make([]string, len(buildLogs))
	copy(logs, buildLogs)
	buildLock.Unlock()

	if c.OnBuildUpdate != nil {
		c.OnBuildUpdate("success", 100, fmt.Sprintf("Build complete! Image: %s", imageName), logs, "", imageName)
	}

	c.logBuild("golden image build complete: %s", workImage)
}

func (c *Controller) failBuild(msg string) {
	c.logBuild("build failed: %s", msg)
	buildLock.Lock()
	buildStatus = "error"
	buildError = msg

	// Capture state
	progress := buildProgress
	step := buildStep
	logs := make([]string, len(buildLogs))
	copy(logs, buildLogs)
	imageName := buildImageName
	buildLock.Unlock()

	if c.OnBuildUpdate != nil {
		c.OnBuildUpdate("error", progress, step, logs, msg, imageName)
	}
}

func ensureDeviceNode(devicePath string) error {
	if _, err := os.Stat(devicePath); err == nil {
		return nil
	}
	// Try to find major:minor from sysfs
	// devicePath e.g. /dev/loop0p2 -> name loop0p2
	deviceName := filepath.Base(devicePath)
	sysPath := fmt.Sprintf("/sys/class/block/%s/dev", deviceName)

	data, err := os.ReadFile(sysPath)
	if err != nil {
		return fmt.Errorf("could not read sysfs for %s: %v", deviceName, err)
	}
	parts := strings.Split(strings.TrimSpace(string(data)), ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid sysfs data for %s: %s", deviceName, string(data))
	}

	// mknod devicePath b major minor
	cmd := exec.Command("mknod", devicePath, "b", parts[0], parts[1])
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mknod failed: %v %s", err, string(out))
	}
	return nil
}

func prepareSSHKeys(rawKey string) (pubKey string, privKeyIndented string) {
	if rawKey == "" {
		return "", ""
	}

	// Try to parse as private key
	signer, err := ssh.ParsePrivateKey([]byte(rawKey))
	if err == nil {
		// It is a valid private key
		pubKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
		// We don't need the private key for the robot anymore
		privKeyIndented = ""
	} else {
		// Parse failed.
		// Check if it looks like a private key to avoid breaking YAML
		if strings.Contains(rawKey, "PRIVATE KEY") || strings.Contains(rawKey, "\n") {
			// It's multiline or looks like a private key, but we couldn't parse it.
			// Do NOT use it as a public key, it will break cloud-init.
			log.Printf("Warning: Failed to parse SSH key and it looks like a private key. Skipping.")
			return "", ""
		}

		// Fallback: assume it is a public key (single line)
		pubKey = strings.TrimSpace(rawKey)
		privKeyIndented = ""
	}
	return
}

func verifyHash(filePath, expectedHash string) bool {
	f, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}

	actualHash := hex.EncodeToString(h.Sum(nil))
	return actualHash == expectedHash
}

func ensureLoopDevices() error {
	for i := 0; i < 8; i++ {
		devPath := fmt.Sprintf("/dev/loop%d", i)
		if _, err := os.Stat(devPath); os.IsNotExist(err) {
			cmd := exec.Command("mknod", devPath, "b", "7", fmt.Sprintf("%d", i))
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to create %s: %v %s", devPath, err, string(out))
			}
		}
	}
	return nil
}

func fetchRemoteHash(imageURL string) (string, error) {
	lastSlash := strings.LastIndex(imageURL, "/")
	if lastSlash == -1 {
		return "", fmt.Errorf("invalid url")
	}
	baseURL := imageURL[:lastSlash+1]
	filename := imageURL[lastSlash+1:]
	sumsURL := baseURL + "SHA256SUMS"

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(sumsURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %s", resp.Status)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, filename) {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				return parts[0], nil
			}
		}
	}
	return "", fmt.Errorf("hash not found in SHA256SUMS")
}
