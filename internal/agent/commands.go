package agent

import (
	"encoding/json"
)

// Command represents a controller-issued instruction handled by an agent.
type Command struct {
	ID         string          `json:"id"`
	ScenarioID int64           `json:"scenario_id,omitempty"`
	Type       string          `json:"type"`
	Data       json.RawMessage `json:"data"`
	Timestamp  int64           `json:"timestamp,omitempty"` // unix seconds, set at publish time
}

// UpdateRepoData describes git repo sync instructions.
type UpdateRepoData struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// ResetLogsData instructs the agent to truncate or remove logs.
type ResetLogsData struct {
	Paths []string `json:"paths"`
}

// ResetBashrcData restores the workspace user's ~/.bashrc to the distro
// default, plus the ROS setup and TurtleBot3 model lines. QtQPAPlatform, when
// set, is exported as QT_QPA_PLATFORM (e.g. "xcb" so rviz2/Gazebo run under
// XWayland on a Wayland desktop).
type ResetBashrcData struct {
	TurtleBot3Model string `json:"turtlebot3_model"`
	QtQPAPlatform   string `json:"qt_qpa_platform,omitempty"`
}

// SystemUpdateData refreshes the package lists (`apt update`), then
// optionally upgrades every installed package and/or installs Packages.
type SystemUpdateData struct {
	Upgrade  bool     `json:"upgrade"`
	Packages []string `json:"packages,omitempty"`
}

// WifiProfileData describes a wifi connection profile.
type WifiProfileData struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

// CaptureImageData describes image capture instructions.
type CaptureImageData struct {
	UploadURL string `json:"upload_url"`
}

// CameraResolutionData selects the ros-camera stream resolution.
type CameraResolutionData struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// TestDriveData describes test drive instructions.
type TestDriveData struct {
	DurationSec int `json:"duration_sec"`
}

// IdentifyData describes identification instructions.
type IdentifyData struct {
	Pattern  string `json:"pattern"`
	Duration int    `json:"duration"`
	// New fields for visual identification
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	IP   string `json:"ip,omitempty"`
	URL  string `json:"url,omitempty"`
}

// SetHostnameData describes an OS hostname update, independent of the
// device's permanent agent identity.
type SetHostnameData struct {
	Hostname string `json:"hostname"`
}

// ConfigureNetworkData describes DDS/ROS networking instructions for a robot
// or laptop's group assignment: which ROS_DOMAIN_ID and RMW implementation to
// use, and optionally a set of static Cyclone DDS discovery peers (its group
// partner's IP, and any lab-manager IPs) in place of multicast discovery.
type ConfigureNetworkData struct {
	ROSDomainID       int      `json:"ros_domain_id"`
	RMWImplementation string   `json:"rmw_implementation"`
	StaticPeers       []string `json:"static_peers,omitempty"`
}

// BatchData describes a list of commands to execute sequentially.
type BatchData struct {
	Commands []Command `json:"commands"`
}
