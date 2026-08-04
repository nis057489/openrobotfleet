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

// imagesDirPath returns the directory golden images are built into and
// served from -- shared by runBuild and the build-cache endpoints so they
// never disagree about where to look. Note this lives on the container's
// ephemeral writable layer (WEB_ROOT), not a persistent volume, so cached
// build progress only survives retries against the same running container.
func imagesDirPath() string {
	webRoot := os.Getenv("WEB_ROOT")
	if webRoot == "" {
		webRoot = "./web/dist"
	}
	return filepath.Join(webRoot, "images")
}

// buildCacheEntry is the JSON-facing view of one in-progress/abandoned
// build's checkpoint, returned by GetGoldenImageBuildCache.
type buildCacheEntry struct {
	ImageName      string    `json:"image_name"`
	RobotModel     string    `json:"robot_model"`
	ROSVersion     string    `json:"ros_version"`
	OverlayEnabled bool      `json:"overlay_enabled"`
	CompletedStage int       `json:"completed_stage"`
	TotalStages    int       `json:"total_stages"`
	UpdatedAt      time.Time `json:"updated_at"`
	SizeBytes      int64     `json:"size_bytes"`
}

// GetGoldenImageBuildCache reports every checkpointed (incomplete) build
// found in imagesDir, not just one matching the currently-loaded config --
// there are only 4 possible (robot model x ROS version) image names, and
// any of them can be independently abandoned mid-build.
func (c *Controller) GetGoldenImageBuildCache(w http.ResponseWriter, r *http.Request) {
	imagesDir := imagesDirPath()
	matches, err := filepath.Glob(filepath.Join(imagesDir, "*.checkpoint.json"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to scan build cache")
		return
	}

	entries := make([]buildCacheEntry, 0, len(matches))
	for _, cpPath := range matches {
		cp, err := loadBuildCheckpoint(cpPath)
		if err != nil {
			log.Printf("golden image build cache: skipping unreadable checkpoint %s: %v", cpPath, err)
			continue
		}
		imageName := strings.TrimSuffix(filepath.Base(cpPath), ".checkpoint.json")
		var sizeBytes int64
		if fi, err := os.Stat(filepath.Join(imagesDir, imageName)); err == nil {
			sizeBytes = fi.Size()
		}
		syntheticCfg := &db.GoldenImageConfig{RobotModel: cp.RobotModel, ROSVersion: cp.ROSVersion, OverlayEnabled: cp.OverlayEnabled}
		entries = append(entries, buildCacheEntry{
			ImageName:      imageName,
			RobotModel:     cp.RobotModel,
			ROSVersion:     cp.ROSVersion,
			OverlayEnabled: cp.OverlayEnabled,
			CompletedStage: cp.CompletedStage,
			TotalStages:    len(buildStages(syntheticCfg)),
			UpdatedAt:      cp.UpdatedAt,
			SizeBytes:      sizeBytes,
		})
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"entries": entries})
}

// ClearGoldenImageBuildCache deletes every checkpointed build's sidecar and
// in-progress image file, forcing the next build (for any config) to start
// completely fresh. Refuses while a build is actively running.
func (c *Controller) ClearGoldenImageBuildCache(w http.ResponseWriter, r *http.Request) {
	buildLock.Lock()
	if buildStatus == "building" {
		buildLock.Unlock()
		respondError(w, http.StatusConflict, "cannot clear build cache while a build is in progress")
		return
	}
	buildLock.Unlock()

	imagesDir := imagesDirPath()
	matches, err := filepath.Glob(filepath.Join(imagesDir, "*.checkpoint.json"))
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to scan build cache")
		return
	}

	for _, cpPath := range matches {
		imageName := strings.TrimSuffix(filepath.Base(cpPath), ".checkpoint.json")
		imagePath := filepath.Join(imagesDir, imageName)
		if err := os.Remove(imagePath); err != nil && !os.IsNotExist(err) {
			log.Printf("clear build cache: failed to remove %s: %v", imagePath, err)
		} else {
			log.Printf("clear build cache: removed %s", imagePath)
		}
		if err := os.Remove(cpPath); err != nil && !os.IsNotExist(err) {
			log.Printf("clear build cache: failed to remove %s: %v", cpPath, err)
		}
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

const userDataTemplate = `#cloud-config
hostname: openrobot
manage_etc_hosts: true
{{if .OverlayEnabled}}
# Overlay builds partition the disk explicitly at build time (golden root
# fixed at ~10GiB, remainder is a separate writable overlay partition -- see
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
set -e
export DEBIAN_FRONTEND=noninteractive

# --- OpenWrt-style overlay root + factory reset ---
# This runs as its own chroot stage (see runChrootStage), separate from the
# workspace-build stage whose cleanup wipes /var/lib/apt/lists -- so the
# package cache here is empty until apt-get update repopulates it.
#
# The package that actually provides /etc/overlayroot.conf support and the
# initramfs local-premount hook is "overlayroot" -- a binary package built
# from the "cloud-initramfs-tools" *source* package, but not installable
# under that source name (apt-get install cloud-initramfs-tools fails with
# "Unable to locate package" regardless of index freshness, since no binary
# package by that name exists). It lives in Ubuntu's "universe" component.
# Nothing else in this pipeline edits sources.list, so universe should
# already be enabled on the stock preinstalled-server-arm64+raspi base image
# -- but log the sources actually in play and make sure universe is on
# (add-apt-repository is idempotent) before assuming apt-get update alone
# will fix a "not found" if this ever regresses again.
echo "--- overlay apt sources diagnostics ---"
cat /etc/apt/sources.list 2>&1 || true
ls /etc/apt/sources.list.d/ 2>&1 || true
echo "--- end overlay apt sources diagnostics ---"
add-apt-repository -y universe
apt-get update
apt-get install -y overlayroot

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
	var workImage, cpPath string
	buildSucceeded := false
	defer func() {
		if r := recover(); r != nil {
			c.failBuild(fmt.Sprintf("panic: %v", r))
		}
		if !buildSucceeded && workImage != "" {
			if cpPath != "" {
				if _, err := os.Stat(cpPath); err == nil {
					c.logBuild("build failed but progress was checkpointed -- leaving %s in place so the next build can resume; use \"Clear cached build progress\" to force a clean rebuild", workImage)
					return
				}
			}
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

	// Compute the deterministic image name/path early (before any
	// network/decompress work) so a resume attempt can detect an
	// in-progress build for this exact config before doing anything else.
	robotModel := cfg.RobotModel
	if robotModel == "" {
		robotModel = "TB3"
	}
	rosVersion := cfg.ROSVersion
	if rosVersion == "" {
		rosVersion = "Humble"
	}
	imageName := fmt.Sprintf("turtlebot-%s-%s-golden.img", strings.ToLower(robotModel), strings.ToLower(rosVersion))

	// 2. Prepare directories
	c.updateBuildProgress("Preparing directories...", 10)
	imagesDir := imagesDirPath()
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		c.failBuild(fmt.Sprintf("mkdir failed: %v", err))
		return
	}
	workImage = filepath.Join(imagesDir, imageName)
	cpPath = checkpointPathFor(workImage)

	// Resume detection: a checkpoint only applies if it matches this exact
	// config (robot model / ROS version / overlay toggle -- the fields that
	// determine install-script content) and the image file it refers to is
	// still on disk (imagesDir is on the container's ephemeral writable
	// layer, not a persistent volume, so this only survives retries against
	// the same running container -- a redeploy just falls back to a normal
	// fresh build).
	resuming := false
	completedStage := 0
	if cp, err := loadBuildCheckpoint(cpPath); err == nil {
		if cp.matches(cfg) {
			if _, statErr := os.Stat(workImage); statErr == nil {
				resuming = true
				completedStage = cp.CompletedStage
				c.logBuild("resuming build for %s from stage %d/%d (checkpoint from %s)", imageName, completedStage, len(buildStages(cfg)), cp.UpdatedAt.Format(time.RFC3339))
			}
		} else {
			c.logBuild("found checkpoint for a different config (model=%s ros=%s overlay=%v); ignoring and starting fresh", cp.RobotModel, cp.ROSVersion, cp.OverlayEnabled)
			os.Remove(cpPath)
		}
	}

	if !resuming {
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
	} else {
		c.logBuild("resuming: reusing existing %s, skipping download/decompress", workImage)
	}

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
	c.updateBuildProgress("Expanding image...", 35)
	if !resuming {
		// truncate -s +N is relative to the file's current size, so this
		// must never re-run against an already-expanded resumed image --
		// doing so would grow it again on every retry.
		expandBy := "+6G"
		if cfg.OverlayEnabled {
			expandBy = "+10G" // ~4GB base + 10G =~ 14GB total, ~900MB margin on a 16GB card
		}
		c.logBuild("expanding image by %s...", expandBy)
		if err := exec.Command("truncate", "-s", expandBy, workImage).Run(); err != nil {
			c.failBuild(fmt.Sprintf("truncate failed: %v", err))
			return
		}
	} else {
		c.logBuild("resuming: image already expanded, skipping")
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
	if !resuming {
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
			// 9GiB previously left workspace-build (colcon's turtlebot3
			// workspace build, run with --parallel-workers 1 so disk usage
			// peaks late and gradually) too little headroom on top of the
			// ros-core stage's package install -- confirmed by a build that
			// completed ros-core cleanly (including its own tinyxml2 sanity
			// checks) and then failed with a bogus-looking "TinyXML2 not
			// found" CMake error mid colcon-build, on a config that only
			// fails with the overlay cap enabled. 10GiB brings this in line
			// with what the non-overlay path already gets (parted resizepart
			// 2 100% against the same ~14GB expanded disk).
			goldenRootEndMiB := p2StartMiB + 10*1024 // ~10GiB golden root

			if out, err := exec.Command("parted", "-s", loopDev, "resizepart", "2", fmt.Sprintf("%dMiB", goldenRootEndMiB)).CombinedOutput(); err != nil {
				c.failBuild(fmt.Sprintf("parted resize golden root failed: %v: %s", err, string(out)))
				return
			}
			if out, err := exec.Command("parted", "-s", loopDev, "mkpart", "primary", "ext4", fmt.Sprintf("%dMiB", goldenRootEndMiB), "100%").CombinedOutput(); err != nil {
				c.failBuild(fmt.Sprintf("parted create overlay partition failed: %v: %s", err, string(out)))
				return
			}
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

	if !resuming {
		c.logBuild("resizing filesystem on %sp2...", loopDev)
		if out, err := exec.Command("resize2fs", loopDev+"p2").CombinedOutput(); err != nil {
			c.failBuild(fmt.Sprintf("resize2fs failed: %v: %s", err, string(out)))
			return
		}
	}

	if cfg.OverlayEnabled {
		if err := ensureDeviceNode(loopDev + "p3"); err != nil {
			c.logBuild("warning: ensureDeviceNode p3: %v", err)
		}
		if !resuming {
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
	}

	// 8. Mount
	c.updateBuildProgress("Mounting image...", 50)
	mntDir := "/mnt/turtlebot-build"
	os.MkdirAll(mntDir, 0755)
	defer os.RemoveAll(mntDir)

	if resuming {
		// Best-effort: a prior attempt may have died hard (OOM/kill -9)
		// without its deferred unmount ever running.
		exec.Command("umount", "-R", mntDir).Run()
	}

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

	// 10. Install ROS 2 & Agent, staged so a failure can resume from the
	// last completed stage instead of redoing the whole hour-long chroot.
	c.updateBuildProgress("Installing ROS 2 and Agent (this takes about an hour)...", 60)

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

	stages := buildStages(cfg)
	const progressStart, progressEnd = 60, 88
	for i, stage := range stages {
		if i < completedStage {
			c.logBuild("skipping already-completed stage: %s", stage.name)
			continue
		}
		pct := progressStart + (progressEnd-progressStart)*i/len(stages)
		c.updateBuildProgress(fmt.Sprintf("Installing (%s, stage %d/%d)...", stage.name, i+1, len(stages)), pct)
		if err := c.runChrootStage(mntDir, stage); err != nil {
			c.failBuild(err.Error())
			return
		}
		exec.Command("sync").Run()
		if err := saveBuildCheckpoint(cpPath, buildCheckpoint{
			RobotModel:     cfg.RobotModel,
			ROSVersion:     cfg.ROSVersion,
			OverlayEnabled: cfg.OverlayEnabled,
			CompletedStage: i + 1,
			UpdatedAt:      time.Now(),
		}); err != nil {
			c.logBuild("warning: failed to save build checkpoint: %v", err)
		}
	}

	// Clean up build artifacts left in the image
	os.Remove(filepath.Join(mntDir, "usr/bin/qemu-aarch64-static"))

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

	// Build is fully complete -- the checkpoint sidecar is no longer needed.
	if err := os.Remove(cpPath); err != nil && !os.IsNotExist(err) {
		c.logBuild("warning: failed to remove build checkpoint: %v", err)
	}

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

// buildStage is one independently-runnable chroot step of the install
// process. Splitting the install into stages (instead of one hour long
// script) lets a failed build resume from the last completed stage instead
// of starting over -- see runBuild's checkpoint handling.
type buildStage struct {
	name   string // stable id, used in checkpoint JSON and log line prefixes
	script string // full, self-contained bash script including its own #!/bin/bash header
}

// buildStages returns the ordered chroot stages for the given config. This
// is the single source of truth for stage order/count: runBuild (to execute
// them) and any future status/cache reporting must both call this rather
// than hardcoding stage counts, so they can never drift out of sync.
func buildStages(cfg *db.GoldenImageConfig) []buildStage {
	var stages []buildStage
	if cfg.RobotModel == "TB4" {
		stages = append(stages,
			buildStage{"turtlebot4-setup", tb4SetupStageScript(cfg)},
			buildStage{"ros-extras", tb4ExtrasStageScript(cfg)},
		)
	} else {
		stages = append(stages, buildStage{"ros-core", tb3CoreStageScript(cfg)})
		if featureEnabled(cfg.CameraEnabled) {
			stages = append(stages, buildStage{"camera-build", tb3CameraStageScript(cfg)})
		}
		stages = append(stages, buildStage{"workspace-build", tb3WorkspaceStageScript(cfg)})
	}
	if cfg.OverlayEnabled {
		stages = append(stages, buildStage{"overlay", overlayInstallScript})
	}
	return stages
}

// featureEnabled treats a nil toggle (any config saved before optional
// feature flags existed) as enabled, so existing golden image configs keep
// building with the full package set they always had.
func featureEnabled(v *bool) bool {
	return v == nil || *v
}

func rosDistroFor(cfg *db.GoldenImageConfig) string {
	if cfg.ROSVersion == "Jazzy" {
		return "jazzy"
	}
	return "humble"
}

func tb4SetupStageScript(cfg *db.GoldenImageConfig) string {
	branch := rosDistroFor(cfg)
	return fmt.Sprintf(`#!/bin/bash
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
rm -f /tmp/turtlebot4_setup.sh
`, branch)
}

func tb4ExtrasStageScript(cfg *db.GoldenImageConfig) string {
	branch := rosDistroFor(cfg)
	return fmt.Sprintf(`#!/bin/bash
set -e
export DEBIAN_FRONTEND=noninteractive

# Defensive re-sync: this stage may run as a separately-resumed process well
# after the ros-core-equivalent stage's apt-get update, so don't assume the
# package lists are still fresh.
apt-get update

# Cyclone DDS is the fleet-wide default RMW; install it alongside whatever
# turtlebot4_setup.sh already configured. v4l2_camera provides both the
# dashboard's camera test and a real ROS image topic for scenarios -- both go
# through ROS rather than a separate direct-V4L2 tool, since the Pi's
# libcamera stack doesn't expose a plain /dev/video0.
apt-get install -y ros-%s-rmw-cyclonedds-cpp ros-%s-compressed-image-transport ros-%s-image-transport-plugins ros-%s-v4l2-camera

# Cleanup
apt-get clean
rm -rf /var/lib/apt/lists/*
`, branch, branch, branch, branch)
}

// tb3CorePackages builds the apt package list for the ros-core stage,
// gating the optional Navigation2/SLAM, teleop, and camera package groups
// behind their respective config toggles so a "quick build" can skip
// packages the student doesn't need (and the disk/time they cost).
func tb3CorePackages(cfg *db.GoldenImageConfig, rosDistro string) string {
	pkgs := []string{
		"ros-base", "turtlebot3-msgs", "dynamixel-sdk", "xacro", "hls-lfcd-lds-driver",
		"robot-state-publisher", "joint-state-publisher", "tf2-tools", "laser-geometry",
		"diagnostic-updater", "rmw-cyclonedds-cpp",
	}
	if featureEnabled(cfg.NavigationEnabled) {
		pkgs = append(pkgs, "slam-toolbox", "navigation2", "nav2-bringup", "cartographer-ros")
	}
	if featureEnabled(cfg.TeleopEnabled) {
		pkgs = append(pkgs, "teleop-twist-keyboard", "teleop-twist-joy", "joy")
	}
	if featureEnabled(cfg.CameraEnabled) {
		pkgs = append(pkgs, "compressed-image-transport", "image-transport-plugins", "v4l2-camera")
	}

	rosPkgs := make([]string, len(pkgs))
	for i, p := range pkgs {
		rosPkgs[i] = fmt.Sprintf("ros-%s-%s", rosDistro, p)
	}
	rosPkgs = append(rosPkgs, "python3-argcomplete", "libboost-system-dev", "libudev-dev",
		"libtinyxml2-dev", "pkg-config", "build-essential", "git", "python3-colcon-common-extensions")
	return strings.Join(rosPkgs, " ")
}

func tb3CoreStageScript(cfg *db.GoldenImageConfig) string {
	rosDistro := rosDistroFor(cfg)
	corePackages := tb3CorePackages(cfg, rosDistro)
	return fmt.Sprintf(`#!/bin/bash
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
apt-get install -y %s

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

# ldconfig has been seen segfaulting under qemu-user aarch64 emulation
# elsewhere in this pipeline (see the camera-build stage) -- this call sits
# directly upstream of the dynamixel_sdk find_library() checks below, so a
# silent segfault here (leaving the library cache/symlinks half-updated)
# is a plausible cause of that failure. Retry rather than assume success.
for attempt in 1 2 3; do
    if ldconfig; then
        break
    elif [ "$attempt" = 3 ]; then
        echo "ldconfig segfaulted 3 times under qemu; giving up"
        exit 1
    else
        echo "ldconfig failed (qemu-user flakiness), retrying ($attempt/3)..."
    fi
done

# turtlebot3_node's colcon build has been seen failing with "Package
# 'dynamixel_sdk' exports the library 'dynamixel_sdk' which couldn't be
# found", thrown by ament_cmake_export_libraries-extras.cmake's find_library()
# call. Confirmed (via a build's diagnostic output) this is NOT a missing/
# corrupted package: dpkg shows it correctly installed and
# libdynamixel_sdk.so is present at exactly the path CMake's find_library()
# should be searching. The failure is specifically in
# turtlebot3_node/CMakeLists.txt's find_package(dynamixel_sdk REQUIRED) call
# (it uses the legacy ${dynamixel_sdk_LIBRARIES} variable style, which
# forces evaluation of that find_library() call) -- something about *this*
# environment (qemu-aarch64 emulation, this exact CMake version, or
# something else) makes it fail even though the file is right there. Rather
# than guess further, reproduce the exact find_package() call in isolation
# (seconds, vs. 15+ minutes into the real colcon build) and log what CMake
# actually resolves, so the next failure (if any) comes with real evidence
# instead of another guess. Still self-heal with a reinstall if the file
# turns out to be genuinely missing, since that's cheap and would explain
# the symptom too.
echo "--- dynamixel_sdk diagnostics ---"
dpkg -l 'ros-*-dynamixel-sdk' 2>&1 || true
DXL_SO="/opt/ros/%s/lib/libdynamixel_sdk.so"
ls -la "$DXL_SO" 2>&1 || true
echo "--- end dynamixel_sdk diagnostics ---"
if [ ! -e "$DXL_SO" ]; then
    echo "warning: libdynamixel_sdk.so missing after initial install; forcing reinstall"
    apt-get install --reinstall -y ros-%s-dynamixel-sdk
    if [ ! -e "$DXL_SO" ]; then
        echo "error: libdynamixel_sdk.so still missing after reinstall"
        exit 1
    fi
    echo "dynamixel_sdk reinstall fixed the missing library"
else
    echo "dynamixel_sdk library present, no reinstall needed"
fi

echo "--- dynamixel_sdk cmake probe ---"
cmake --version
cat /opt/ros/%s/share/dynamixel_sdk/cmake/ament_cmake_export_libraries-extras.cmake 2>&1 || true
mkdir -p /tmp/dxl_probe
cat > /tmp/dxl_probe/CMakeLists.txt <<'PROBEEOF'
cmake_minimum_required(VERSION 3.5)
project(dxl_probe)
find_package(dynamixel_sdk REQUIRED)
message(STATUS "PROBE dynamixel_sdk_DIR=${dynamixel_sdk_DIR}")
message(STATUS "PROBE dynamixel_sdk_LIBRARIES=${dynamixel_sdk_LIBRARIES}")
message(STATUS "PROBE dynamixel_sdk_INCLUDE_DIRS=${dynamixel_sdk_INCLUDE_DIRS}")
PROBEEOF
( . /opt/ros/%s/setup.bash && cmake -S /tmp/dxl_probe -B /tmp/dxl_probe/build ) || echo "PROBE cmake configure failed (see above -- this reproduces the real failure in isolation)"
rm -rf /tmp/dxl_probe
echo "--- end dynamixel_sdk cmake probe ---"

# Free the downloaded .deb cache from the ROS/nav2/cartographer install
# above before the colcon build, which needs its own disk for build
# artifacts. Package lists get re-fetched at the very end if anything else
# needs apt again, so this is safe mid-script.
apt-get clean
`, corePackages, rosDistro, rosDistro, rosDistro, rosDistro)
}

func tb3CameraStageScript(cfg *db.GoldenImageConfig) string {
	rosDistro := rosDistroFor(cfg)
	return fmt.Sprintf(`#!/bin/bash
set -e
export DEBIAN_FRONTEND=noninteractive

# Defensive re-sync: this stage may run as a separately-resumed process well
# after the ros-core stage's apt-get update, so don't assume the package
# lists are still fresh.
apt-get update

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

# ldconfig has been seen segfaulting here with "qemu: uncaught target
# signal 11" -- a known qemu-user-static flakiness under aarch64 emulation,
# not anything wrong with the cache it's building. ldconfig is idempotent,
# so just retry it a couple of times before giving up.
for attempt in 1 2 3; do
    if ldconfig; then
        break
    elif [ "$attempt" = 3 ]; then
        echo "ldconfig segfaulted 3 times under qemu; giving up"
        exit 1
    else
        echo "ldconfig failed (qemu-user flakiness), retrying ($attempt/3)..."
    fi
done
apt-get clean
`, rosDistro)
}

func tb3WorkspaceStageScript(cfg *db.GoldenImageConfig) string {
	rosDistro := rosDistroFor(cfg)
	return fmt.Sprintf(`#!/bin/bash
set -e
export DEBIAN_FRONTEND=noninteractive

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

# colcon build has been seen failing on turtlebot3_node with the same
# "dynamixel_sdk exports the library ... which couldn't be found" CMake
# error diagnosed above, even though an isolated CMake probe against the
# same library resolves fine. A retry of the plain colcon build reproduces
# the identical failure in a fraction of the time, which rules out simple
# environmental flakiness -- something in the real colcon-driven configure
# differs from the isolated probe. Since the chroot (and this build
# directory) gets torn down the moment the stage fails, capture the actual
# CMake trace and cache state for turtlebot3_node's configure right here,
# before that happens, instead of guessing further.
#
# Also seen: unrelated-looking CMake "package not found" errors (e.g.
# TinyXML2) on turtlebot3_description, on overlay/read-only builds only --
# that build's root partition is capped at a fixed size (see runBuild's
# partitioning step) rather than filling the disk, and colcon's disk usage
# peaks right around here. Log free space up front so a future recurrence is
# unambiguous instead of another red herring chase through CMake output.
echo "--- disk usage before colcon build ---"
df -h /
echo "--- end disk usage before colcon build ---"
if ! colcon build --symlink-install --parallel-workers 1; then
    echo "colcon build failed; capturing diagnostics before the chroot is torn down..."
    echo "--- disk usage at failure ---"
    df -h /
    echo "--- end disk usage at failure ---"
    NODE_BUILD_DIR=/home/ubuntu/ros_ws/build/turtlebot3_node
    echo "--- turtlebot3_node CMakeCache.txt (dynamixel_sdk entries) ---"
    grep -i dynamixel "$NODE_BUILD_DIR/CMakeCache.txt" 2>&1 || true
    echo "--- turtlebot3_node CMakeError.log (tail) ---"
    tail -n 100 "$NODE_BUILD_DIR/CMakeFiles/CMakeError.log" 2>&1 || true
    echo "--- re-running turtlebot3_node configure with --trace-expand ---"
    ( cd "$NODE_BUILD_DIR" && cmake --trace-expand -S /home/ubuntu/ros_ws/src/turtlebot3/turtlebot3_node -B . 2>&1 | grep -B5 -A20 "dynamixel_sdk' exports the library" ) || true
    echo "--- end turtlebot3_node CMake diagnostics ---"
    exit 1
fi
chown -R ubuntu:ubuntu /home/ubuntu/ros_ws
chown ubuntu:ubuntu /home/ubuntu
mkdir -p /home/ubuntu/.ros
chown -R ubuntu:ubuntu /home/ubuntu/.ros

# Udev Rules
cp /home/ubuntu/ros_ws/src/turtlebot3/turtlebot3_bringup/script/99-turtlebot3-cdc.rules /etc/udev/rules.d/

# Cleanup
apt-get clean
rm -rf /var/lib/apt/lists/*
`, rosDistro, rosDistro, rosDistro, rosDistro)
}

// runChrootStage writes one build stage's script into the chroot and runs
// it, streaming output into the build log with a "[stage-name]" prefix.
// Used identically whether this is a fresh stage or a first attempt at a
// resumed one.
func (c *Controller) runChrootStage(mntDir string, stage buildStage) error {
	scriptPath := filepath.Join(mntDir, "tmp/install-stage.sh")
	if err := os.WriteFile(scriptPath, []byte(stage.script), 0755); err != nil {
		return fmt.Errorf("write %s stage script: %w", stage.name, err)
	}
	defer os.Remove(scriptPath)

	c.logBuild("=== stage: %s ===", stage.name)
	cmd := exec.Command("chroot", mntDir, "/bin/bash", "/tmp/install-stage.sh")
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("stage %s start: %w", stage.name, err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			c.logBuild("[%s] %s", stage.name, scanner.Text())
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			c.logBuild("[%s/err] %s", stage.name, scanner.Text())
		}
	}()
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("stage %s failed: %w", stage.name, err)
	}
	return nil
}

// buildCheckpoint tracks how far a chroot install got, so a retry against
// the same workImage can resume instead of starting over. Stored as a small
// JSON sidecar next to workImage -- see runBuild's resume-detection block.
type buildCheckpoint struct {
	RobotModel     string    `json:"robot_model"`
	ROSVersion     string    `json:"ros_version"`
	OverlayEnabled bool      `json:"overlay_enabled"`
	CompletedStage int       `json:"completed_stage"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func checkpointPathFor(workImage string) string {
	return workImage + ".checkpoint.json"
}

func loadBuildCheckpoint(path string) (*buildCheckpoint, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp buildCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func saveBuildCheckpoint(path string, cp buildCheckpoint) error {
	data, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (cp *buildCheckpoint) matches(cfg *db.GoldenImageConfig) bool {
	return cp.RobotModel == cfg.RobotModel && cp.ROSVersion == cfg.ROSVersion && cp.OverlayEnabled == cfg.OverlayEnabled
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
