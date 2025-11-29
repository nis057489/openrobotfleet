package agent

import "encoding/json"

// Command represents a controller-issued instruction handled by an agent.
type Command struct {
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
	Duration int `json:"duration"` // seconds
}
