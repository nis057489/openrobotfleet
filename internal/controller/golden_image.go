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
	"regexp"
	"strconv"
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
{{if .OverlayEnabled}}
# Overlay builds partition the disk explicitly at build time (golden root
# fixed at ~9GiB, remainder is a separate writable overlay partition -- see
# runBuild's partitioning step). growpart resizing "/" at first boot is
# incompatible with that fixed layout, so both it and the matching resizefs
# step are disabled here.
growpart:
  mode: "off"
resize_rootfs: false
{{end}}
users:
  - name: ubuntu
    groups: [sudo, dialout, video]
    shell: /bin/bash
    sudo: ['ALL=(ALL) NOPASSWD:ALL']
    lock_passwd: false
    ssh_authorized_keys:
      {{if .SSHPublicKey}}- {{.SSHPublicKey}}{{end}}
{{if .UbuntuPassword}}
chpasswd:
  expire: false
  list:
    - ubuntu:{{.UbuntuPassword}}
{{end}}
# Packages are pre-installed in the golden image.
# We only handle runtime configuration here.

write_files:
  - path: /usr/local/bin/openrobotfleet-agent-start
    permissions: '0755'
    content: |
      #!/bin/bash
      for setup in /opt/ros/*/setup.bash; do
        [ -f "$setup" ] && source "$setup" && break
      done
      # Without this, every ros2 command the agent runs (test_drive, identify)
      # defaults to ROS_DOMAIN_ID 0 with the default RMW, completely
      # disconnected from the domain ros.service actually bringup runs on.
      # Keep this in sync with agentStartScript in internal/ssh/ssh.go, used
      # by the SSH-based install/reinstall path.
      [ -f /etc/openrobotfleet-agent/ros_env.sh ] && source /etc/openrobotfleet-agent/ros_env.sh
      exec /usr/local/bin/openrobotfleet-agent

  - path: /usr/local/bin/openrobotfleet-ros-start
    permissions: '0755'
    content: |
      #!/bin/bash
      for setup in /opt/ros/*/setup.bash; do
        [ -f "$setup" ] && source "$setup" && break
      done
      [ -f /home/ubuntu/ros_ws/install/setup.bash ] && source /home/ubuntu/ros_ws/install/setup.bash
      [ -f /etc/openrobotfleet-agent/ros_env.sh ] && source /etc/openrobotfleet-agent/ros_env.sh
      export LDS_MODEL={{.LDSModel}}
      export TURTLEBOT3_MODEL=waffle_pi
      exec ros2 launch turtlebot3_bringup robot.launch.py

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

  - path: /etc/openrobotfleet-agent/ros_env.sh
    content: |
      export RMW_IMPLEMENTATION=rmw_cyclonedds_cpp
      export ROS_DOMAIN_ID={{.ROSDomainID}}

  - path: /home/ubuntu/ros_ws/qos_overrides.example.yaml
    content: |
      # Example QoS overrides for continuously-streaming sensor topics, per the
      # lab network plan: Best Effort / Keep Last 1 / Volatile for sensors that
      # stream constantly (LaserScan, Camera, IMU, Odometry, JointState), while
      # cmd_vel/services/actions stay on the ROS 2 Reliable default. Reference
      # this from your scenario's launch file, e.g.:
      #   ros2 launch <pkg> <file>.launch.py --ros-args --params-file qos_overrides.example.yaml
      /**:
        ros__parameters:
          qos_overrides:
            /scan:
              publisher:
                reliability: best_effort
                history: keep_last
                depth: 1
                durability: volatile
            /camera/image_raw/compressed:
              publisher:
                reliability: best_effort
                history: keep_last
                depth: 1
                durability: volatile
            /imu:
              publisher:
                reliability: best_effort
                history: keep_last
                depth: 1
                durability: volatile
            /odom:
              publisher:
                reliability: best_effort
                history: keep_last
                depth: 1
                durability: volatile
            /joint_states:
              publisher:
                reliability: best_effort
                history: keep_last
                depth: 1
                durability: volatile

  - path: /home/ubuntu/ros_ws/camera_params.example.yaml
    content: |
      # For the onboard Pi camera (a raw Bayer CSI sensor), use camera_ros --
      # it's the libcamera-backed driver, which is what actually knows how to
      # configure the sensor and run frames through the ISP for demosaicing:
      #   ros2 run camera_ros camera_node
      # v4l2_camera works too, but only for a plain USB/UVC webcam (one that
      # already outputs ready-to-use YUYV/MJPEG); it can't drive the CSI
      # sensor's raw Bayer pipeline. If you have a USB webcam instead, this
      # image_size param file applies there:
      #   ros2 run v4l2_camera v4l2_camera_node --ros-args --params-file camera_params.example.yaml
      # Nothing starts a camera node automatically -- it would hold the
      # device open and block both the dashboard's Test Camera button and any
      # camera node your scenario launches -- so start one yourself when you
      # want a live stream. The /camera/image_raw/compressed topic is
      # published automatically alongside the raw one once
      # compressed_image_transport is installed.
      /**:
        ros__parameters:
          image_size: [320, 240]

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

  # A robot should never suspend/hibernate mid-mission
  - systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target

  # Environment variables
  {{if eq .RobotModel "TB4"}}
  # TB4 setup script handles its own ROS env vars; ours (Cyclone DDS RMW +
  # this group's ROS_DOMAIN_ID) is appended last below, so it wins.
  {{else}}
  # TB3 Default
  - echo 'source /opt/ros/{{if eq .ROSVersion "Jazzy"}}jazzy{{else}}humble{{end}}/setup.bash' >> /home/ubuntu/.bashrc
  - echo 'source /home/ubuntu/ros_ws/install/setup.bash' >> /home/ubuntu/.bashrc
  - echo 'export LDS_MODEL={{.LDSModel}}' >> /home/ubuntu/.bashrc
  {{end}}
  # RMW_IMPLEMENTATION and ROS_DOMAIN_ID are re-applied at runtime whenever a
  # robot is assigned to a Group (see the configure_network agent command);
  # this is just the pre-grouping default so ungrouped robots still isolate
  # reasonably out of the box.
  - echo 'source /etc/openrobotfleet-agent/ros_env.sh' >> /home/ubuntu/.bashrc
  - chown ubuntu:ubuntu /home/ubuntu/ros_ws/qos_overrides.example.yaml
  - chown ubuntu:ubuntu /home/ubuntu/ros_ws/camera_params.example.yaml

  # Fix home directory and ROS permissions
  - chown ubuntu:ubuntu /home/ubuntu
  - mkdir -p /home/ubuntu/.ros
  - chown -R ubuntu:ubuntu /home/ubuntu/.ros

  # Agent Service (Binary is pre-installed)
  - |
    cat <<EOF > /etc/systemd/system/openrobotfleet-agent.service
    [Unit]
    Description=OpenRobot Agent
    After=network.target

    [Service]
    ExecStart=/usr/local/bin/openrobotfleet-agent-start
    Restart=always
    User=root
    Environment=AGENT_CONFIG_PATH=/etc/openrobotfleet-agent/config.yaml

    [Install]
    WantedBy=multi-user.target
    EOF
  - systemctl enable openrobotfleet-agent
  - systemctl start openrobotfleet-agent

  {{if ne .RobotModel "TB4"}}
  # ROS Bringup Service (turtlebot3_bringup: motor/LDS drivers, TF, odometry).
  # Named "ros" to match the agent's default ROS_SERVICE_NAME so the
  # dashboard's "Restart ROS" command works without extra configuration.
  # Restart=on-failure (not "always") avoids a tight crash loop if OpenCR
  # isn't connected yet; it'll pick back up once it is.
  - |
    cat <<EOF > /etc/systemd/system/ros.service
    [Unit]
    Description=OpenRobot ROS Bringup (turtlebot3_bringup)
    After=network.target

    [Service]
    Type=simple
    User=ubuntu
    Environment=HOME=/home/ubuntu
    ExecStart=/usr/local/bin/openrobotfleet-ros-start
    Restart=on-failure
    RestartSec=10

    [Install]
    WantedBy=multi-user.target
    EOF
  - systemctl enable ros
  - systemctl start ros
  {{end}}

final_message: "OpenRobot setup complete. Ready to roll!"
`

// overlayInstallScript is appended (only when cfg.OverlayEnabled) as the very
// last step of the chroot install script, after everything else -- including
// any package upgrades -- has finished, so the initramfs it builds reflects
// the image's truly final state. It makes the golden root (partition 2)
// read-only at runtime via overlayroot, backed by a writable overlay on
// partition 3 (created in runBuild's partitioning step), and installs a
// fail-safe initramfs hook that wipes just the overlay when a
// factory-reset-requested flag file is found on the boot partition. Every
// path through the wipe-check script below falls through to a normal boot --
// there is no remote recovery from a Pi that won't boot, so a broken or
// missing overlay device must never be treated as fatal.
const overlayInstallScript = `
# --- OpenWrt-style overlay root + factory reset ---
apt-get install -y cloud-initramfs-tools

cat <<'OVERLAYEOF' > /etc/overlayroot.conf
overlayroot_cfgdisk="disabled"
overlayroot="device:dev=LABEL=golden-overlay,recurse=0"
OVERLAYEOF

# recurse=0 is required on the Raspberry Pi: the default recurse=1 also makes
# /boot/firmware read-only, which breaks flash-kernel and produces an
# unbootable image.

mkdir -p /etc/initramfs-tools/hooks
cat <<'HOOKEOF' > /etc/initramfs-tools/hooks/factory-reset-mkfs
#!/bin/sh
PREREQ=""
prereqs() { echo "$PREREQ"; }
case $1 in
prereqs)
    prereqs
    exit 0
    ;;
esac

. /usr/share/initramfs-tools/hook-functions

copy_exec /sbin/mkfs.ext4
copy_exec /sbin/mke2fs
copy_exec /sbin/blkid

exit 0
HOOKEOF
chmod +x /etc/initramfs-tools/hooks/factory-reset-mkfs

# The "00-" prefix is deliberate: initramfs-tools runs local-premount scripts
# in lexical order, and the overlay device must be wiped (if a reset was
# requested) before overlayroot's own script tries to mount it.
mkdir -p /etc/initramfs-tools/scripts/local-premount
cat <<'PREMOUNTEOF' > /etc/initramfs-tools/scripts/local-premount/00-factory-reset-check
#!/bin/sh
PREREQ=""
prereqs() { echo "$PREREQ"; }
case $1 in
prereqs)
    prereqs
    exit 0
    ;;
esac

log() {
    echo "factory-reset-check: $*" > /dev/kmsg 2>/dev/null || true
}

# This entire script must never block boot: every path below falls through to
# "exit 0". A robot with no remote recovery must always come back up.
BOOT_MNT="/mnt/factory-reset-boot"
mkdir -p "$BOOT_MNT" 2>/dev/null

BOOT_DEV=$(blkid -L system-boot 2>/dev/null)
if [ -z "$BOOT_DEV" ]; then
    BOOT_DEV=$(blkid -t TYPE=vfat -o device 2>/dev/null | head -n1)
fi
if [ -z "$BOOT_DEV" ]; then
    log "no boot partition found, skipping"
    exit 0
fi

if ! mount -t vfat -o rw "$BOOT_DEV" "$BOOT_MNT" 2>/dev/null; then
    log "failed to mount boot partition $BOOT_DEV, skipping"
    exit 0
fi

FLAG="$BOOT_MNT/factory-reset-requested"
if [ ! -f "$FLAG" ]; then
    umount "$BOOT_MNT" 2>/dev/null
    exit 0
fi

log "factory reset flag found, looking for overlay device"
OVERLAY_DEV=$(blkid -L golden-overlay 2>/dev/null)
if [ -z "$OVERLAY_DEV" ]; then
    log "overlay device (LABEL=golden-overlay) not found, leaving flag for retry"
    umount "$BOOT_MNT" 2>/dev/null
    exit 0
fi

log "wiping overlay device $OVERLAY_DEV"
if mkfs.ext4 -F -L golden-overlay "$OVERLAY_DEV" >/dev/kmsg 2>&1; then
    log "overlay wiped successfully, clearing flag"
    rm -f "$FLAG"
else
    log "mkfs.ext4 failed, leaving flag for retry"
fi
umount "$BOOT_MNT" 2>/dev/null
exit 0
PREMOUNTEOF
chmod +x /etc/initramfs-tools/scripts/local-premount/00-factory-reset-check

update-initramfs -u -k all
`

var (
	buildLock      sync.Mutex
	buildStatus    = "idle" // idle, building, success, error
	buildError     string
	buildProgress  int    // 0-100
	buildStep      string // Current step description
	buildLogs      []string
	buildImageName string
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
	var workImage string
	buildSucceeded := false
	defer func() {
		if r := recover(); r != nil {
			c.failBuild(fmt.Sprintf("panic: %v", r))
		}
		if !buildSucceeded && workImage != "" {
			c.logBuild("cleaning up failed work image: %s", workImage)
			os.Remove(workImage)
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
	workImage = filepath.Join(imagesDir, imageName)

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

	// 5. Expand Image. Sized for 16GB SD cards: base preinstalled image
	// (~4GB) + ROS Humble/nav2/cartographer/slam-toolbox/camera packages +
	// colcon build artifacts (~5-6GB) still leaves several GB free for
	// student code, logs, and scenario repos.
	//
	// Overlay builds use a larger, fixed absolute expansion instead of
	// relying on cloud-init's growpart at first boot: growpart's behavior
	// once "/" is an overlayfs mount assembled in initramfs is untested/
	// unsafe territory, so overlay images are partitioned explicitly and
	// statically at build time instead (see step 7).
	expandBy := "+6G"
	if cfg.OverlayEnabled {
		expandBy = "+10G" // ~4GB base + 10G =~ 14GB total, ~900MB margin on a 16GB card
	}
	c.updateBuildProgress("Expanding image...", 35)
	c.logBuild("expanding image by %s...", expandBy)
	if err := exec.Command("truncate", "-s", expandBy, workImage).Run(); err != nil {
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

	if !cfg.OverlayEnabled {
		if out, err := exec.Command("parted", "-s", loopDev, "resizepart", "2", "100%").CombinedOutput(); err != nil {
			c.failBuild(fmt.Sprintf("parted failed: %v: %s", err, string(out)))
			return
		}
	} else {
		// Golden root (partition 2) is capped at a fixed size instead of
		// filling the disk, and the remainder becomes a new partition 3 --
		// the writable overlay. Parse partition 2's current start offset
		// rather than hardcoding it; Ubuntu has changed Pi image partition
		// layouts between releases before.
		printOut, err := exec.Command("parted", "-s", loopDev, "unit", "MiB", "print").CombinedOutput()
		if err != nil {
			c.failBuild(fmt.Sprintf("parted print failed: %v: %s", err, string(printOut)))
			return
		}
		c.logBuild("partition table before resize:\n%s", string(printOut))
		p2StartMiB, err := parsePartitionStartMiB(string(printOut), 2)
		if err != nil {
			c.failBuild(fmt.Sprintf("failed to determine partition 2 start: %v", err))
			return
		}
		goldenRootEndMiB := p2StartMiB + 9*1024 // ~9GiB golden root

		if out, err := exec.Command("parted", "-s", loopDev, "resizepart", "2", fmt.Sprintf("%dMiB", goldenRootEndMiB)).CombinedOutput(); err != nil {
			c.failBuild(fmt.Sprintf("parted resize golden root failed: %v: %s", err, string(out)))
			return
		}
		if out, err := exec.Command("parted", "-s", loopDev, "mkpart", "primary", "ext4", fmt.Sprintf("%dMiB", goldenRootEndMiB), "100%").CombinedOutput(); err != nil {
			c.failBuild(fmt.Sprintf("parted create overlay partition failed: %v: %s", err, string(out)))
			return
		}
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

	if cfg.OverlayEnabled {
		if err := ensureDeviceNode(loopDev + "p3"); err != nil {
			c.logBuild("warning: ensureDeviceNode p3: %v", err)
		}
		// Pre-format the overlay partition now. overlayroot does not
		// auto-mkfs at boot; this LABEL is what /etc/overlayroot.conf's
		// dev=LABEL=... references, and what the factory-reset wipe-hook
		// re-formats on demand (see the installScript overlay setup below).
		c.logBuild("formatting overlay partition %sp3...", loopDev)
		if out, err := exec.Command("mkfs.ext4", "-F", "-L", "golden-overlay", loopDev+"p3").CombinedOutput(); err != nil {
			c.failBuild(fmt.Sprintf("mkfs overlay partition failed: %v: %s", err, string(out)))
			return
		}
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
apt-get upgrade -y
apt-get install -y wget curl git

# Download and run official setup script
wget -qO /tmp/turtlebot4_setup.sh https://raw.githubusercontent.com/turtlebot/turtlebot4_setup/%s/scripts/turtlebot4_setup.sh
bash /tmp/turtlebot4_setup.sh

# Cyclone DDS is the fleet-wide default RMW; install it alongside whatever
# turtlebot4_setup.sh already configured. v4l2_camera provides both the
# dashboard's camera test and a real ROS image topic for scenarios -- both go
# through ROS rather than a separate direct-V4L2 tool, since the Pi's
# libcamera stack doesn't expose a plain /dev/video0.
apt-get install -y ros-%s-rmw-cyclonedds-cpp ros-%s-compressed-image-transport ros-%s-image-transport-plugins ros-%s-v4l2-camera

# Cleanup
rm -f /tmp/turtlebot4_setup.sh /tmp/install.sh
apt-get clean
rm -rf /var/lib/apt/lists/*
`, branch, branch, branch, branch, branch)
	} else {
		// TB3 Logic
		rosDistro := "humble"
		if cfg.ROSVersion == "Jazzy" {
			rosDistro = "jazzy"
		}
		installScript = fmt.Sprintf(`#!/bin/bash
set -e
export DEBIAN_FRONTEND=noninteractive

# Install ROS 2
apt-get update
apt-get upgrade -y
apt-get install -y software-properties-common curl gnupg lsb-release
curl -sSL https://raw.githubusercontent.com/ros/rosdistro/master/ros.key -o /usr/share/keyrings/ros-archive-keyring.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/ros-archive-keyring.gpg] http://packages.ros.org/ros2/ubuntu $(source /etc/os-release && echo $UBUNTU_CODENAME) main" | tee /etc/apt/sources.list.d/ros2.list > /dev/null
apt-get update

# Work around a package-version mismatch on Ubuntu Noble/arm64 that can block
# ROS dependency resolution with libzstd during golden-image builds.
if ! apt-get install -y --allow-downgrades libzstd1=1.5.5+dfsg2-2build1 libzstd-dev=1.5.5+dfsg2-2build1; then
    echo "warning: could not pin libzstd versions; continuing with the standard dependency resolution"
fi
apt-get install -y --fix-broken
apt-get install -y ros-%s-ros-base ros-%s-turtlebot3-msgs ros-%s-dynamixel-sdk ros-%s-xacro ros-%s-hls-lfcd-lds-driver ros-%s-slam-toolbox ros-%s-navigation2 ros-%s-nav2-bringup ros-%s-cartographer-ros ros-%s-teleop-twist-keyboard ros-%s-teleop-twist-joy ros-%s-joy ros-%s-robot-state-publisher ros-%s-joint-state-publisher ros-%s-tf2-tools ros-%s-laser-geometry ros-%s-diagnostic-updater ros-%s-rmw-cyclonedds-cpp ros-%s-compressed-image-transport ros-%s-image-transport-plugins ros-%s-v4l2-camera python3-argcomplete libboost-system-dev libudev-dev libtinyxml2-dev pkg-config build-essential git python3-colcon-common-extensions

# packages.ros.org ships a newer libtinyxml2-dev than Ubuntu jammy's own
# tinyxml2 runtime, and apt's dependency resolution between the two isn't
# consistent build to build: sometimes the unversioned libtinyxml2.so symlink
# CMake's find_library()/pkg-config need ends up missing or pointing at a
# stale/incompatible file. Log exactly what's on disk so a repeat failure is
# debuggable from the build log alone, then force the symlink to the newest
# .so present regardless of what (if anything) is already there.
echo "--- tinyxml2 diagnostics ---"
dpkg -l 'libtinyxml2*' 2>&1 || true
find / -xdev -iname 'libtinyxml2*' 2>/dev/null || true
find / -xdev -iname 'tinyxml2.pc' 2>/dev/null || true
echo "--- end tinyxml2 diagnostics ---"
TINYXML2_SO=$(find /usr/lib -name 'libtinyxml2.so.*' | sort -V | tail -1)
if [ -n "$TINYXML2_SO" ]; then
    ln -sf "$(basename "$TINYXML2_SO")" "$(dirname "$TINYXML2_SO")/libtinyxml2.so"
    echo "linked libtinyxml2.so -> $(basename "$TINYXML2_SO")"
else
    echo "warning: no libtinyxml2.so.* found under /usr/lib; turtlebot3_description build will likely fail"
fi
ldconfig

# Free the downloaded .deb cache from the ROS/nav2/cartographer install
# above before the colcon build, which needs its own disk for build
# artifacts. Package lists get re-fetched at the very end if anything else
# needs apt again, so this is safe mid-script.
apt-get clean

# camera_ros (libcamera-based ROS camera driver). v4l2_camera can't produce a
# real image from this Bayer CSI sensor on its own -- it only sets the video
# node's format, never configures the sensor subdevice pad or routes frames
# through the ISP for demosaicing, so streaming fails outright and even if it
# didn't, the output would be raw, uncorrected Bayer data. libcamera is what
# actually knows how to drive this pipeline; camera_ros just wraps it as a
# ROS node. Jammy's own libcamera is too old for camera_ros, hence building
# the Raspberry Pi fork from source.
apt-get install -y python3-pip python3-jinja2 python3-yaml python3-ply \
    libboost-dev libgnutls28-dev openssl libtiff-dev pybind11-dev \
    qtbase5-dev libqt5core5a libqt5widgets5 meson cmake \
    libglib2.0-dev libgstreamer-plugins-base1.0-dev
apt-get install -y ros-%s-camera-ros

# Jammy's apt meson (0.61) is too old for this libcamera (needs >= 0.63);
# pip's meson is newer and installs to /usr/local/bin, which takes PATH
# precedence over apt's /usr/bin/meson.
pip3 install --upgrade 'meson>=0.63'

git clone -b v0.5.2 --depth 1 https://github.com/raspberrypi/libcamera.git /tmp/libcamera
cd /tmp/libcamera
meson setup build --buildtype=release -Dpipelines=rpi/vc4,rpi/pisp -Dipas=rpi/vc4,rpi/pisp -Dv4l2=true -Dgstreamer=enabled -Dtest=false -Dlc-compliance=disabled -Dcam=disabled -Dqcam=disabled -Ddocumentation=disabled -Dpycamera=enabled
ninja -C build -j 1
ninja -C build install -j 1
cd /
rm -rf /tmp/libcamera

# ninja install puts libcamera under /usr/local/lib/<triplet>; make it a
# permanent part of the linker's search path via ld.so.conf.d instead of an
# env var, so every process (agent, ros.service, an interactive shell) picks
# it up automatically without each needing to know to export LD_LIBRARY_PATH.
echo "/usr/local/lib/$(dpkg-architecture -qDEB_HOST_MULTIARCH)" > /etc/ld.so.conf.d/openrobotfleet-libcamera.conf
ldconfig
apt-get clean

# Setup Workspace
if ! id -u ubuntu >/dev/null 2>&1; then
    useradd --create-home --shell /bin/bash --groups sudo ubuntu
fi
mkdir -p /home/ubuntu/ros_ws/src
cd /home/ubuntu/ros_ws/src
git clone -b %s https://github.com/ROBOTIS-GIT/turtlebot3.git
git clone -b %s https://github.com/ROBOTIS-GIT/ld08_driver.git
git clone -b %s https://github.com/ROBOTIS-GIT/coin_d4_driver.git

# turtlebot3_cartographer/turtlebot3_navigation2 are thin example packages
# that duplicate the full cartographer-ros/navigation2 packages already
# installed above; building them from source here roughly doubles build time
# for no benefit (per ROBOTIS's own setup instructions).
rm -rf turtlebot3/turtlebot3_cartographer turtlebot3/turtlebot3_navigation2

cd /home/ubuntu/ros_ws
source /opt/ros/%s/setup.bash
colcon build --symlink-install --parallel-workers 1
chown -R ubuntu:ubuntu /home/ubuntu/ros_ws
chown ubuntu:ubuntu /home/ubuntu
mkdir -p /home/ubuntu/.ros
chown -R ubuntu:ubuntu /home/ubuntu/.ros

# Udev Rules
cp /home/ubuntu/ros_ws/src/turtlebot3/turtlebot3_bringup/script/99-turtlebot3-cdc.rules /etc/udev/rules.d/

# Cleanup
rm -f /tmp/install.sh
apt-get clean
rm -rf /var/lib/apt/lists/*
`, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro, rosDistro)
	}
	if cfg.OverlayEnabled {
		installScript += overlayInstallScript
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

	// Clean up build artifacts left in the image
	os.Remove(filepath.Join(mntDir, "usr/bin/qemu-aarch64-static"))
	os.Remove(filepath.Join(mntDir, "tmp/install.sh"))

	// Restore resolv.conf to the Ubuntu default symlink (we replaced it with the build host's copy)
	os.Remove(filepath.Join(mntDir, "etc/resolv.conf"))
	os.Symlink("/run/systemd/resolve/stub-resolv.conf", filepath.Join(mntDir, "etc/resolv.conf"))

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

	buildSucceeded = true

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

// parsePartitionStartMiB extracts the start offset (in MiB, truncated to an
// int) of the given partition number from `parted -s <dev> unit MiB print`
// output. Parted's row format is e.g. " 2      512MiB    4096MiB   ...", with
// the start value occasionally fractional (e.g. "1.00MiB").
func parsePartitionStartMiB(partedOutput string, partNum int) (int, error) {
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*%d\s+(\d+(?:\.\d+)?)MiB`, partNum))
	matches := re.FindStringSubmatch(partedOutput)
	if len(matches) < 2 {
		return 0, fmt.Errorf("partition %d not found in parted output", partNum)
	}
	startMiB, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("parse start offset %q: %w", matches[1], err)
	}
	return int(startMiB), nil
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
