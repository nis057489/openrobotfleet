package agent

import "encoding/json"

// Command represents a controller-issued instruction handled by an agent.
type Command struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
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

// WifiProfileData describes a wifi connection profile.
type WifiProfileData struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

// CaptureImageData describes image capture instructions.
type CaptureImageData struct {
	UploadURL string `json:"upload_url"`
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

// ConfigureAgentData describes agent configuration instructions.
type ConfigureAgentData struct {
	AgentID string `json:"agent_id"`
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
