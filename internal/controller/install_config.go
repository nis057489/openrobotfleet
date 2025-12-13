package controller

import (
	"errors"
	"strings"

	"example.com/turtlebot-fleet/internal/db"
)

type installConfigRequest struct {
	Address string `json:"address"`
	User    string `json:"user"`
	SSHKey  string `json:"ssh_key"`
}

func (req installConfigRequest) validate() error {
	if strings.TrimSpace(req.Address) == "" || strings.TrimSpace(req.User) == "" || strings.TrimSpace(req.SSHKey) == "" {
		return errors.New("address, user, and ssh_key required")
	}
	if !strings.Contains(req.SSHKey, "-----BEGIN OPENSSH PRIVATE KEY-----") || !strings.Contains(req.SSHKey, "-----END OPENSSH PRIVATE KEY-----") {
		return errors.New("ssh_key must be a valid OPENSSH private key")
	}
	return nil
}

type installDefaultsRequest struct {
	User   string `json:"user"`
	SSHKey string `json:"ssh_key"`
}

func (req installDefaultsRequest) validate() error {
	if strings.TrimSpace(req.User) == "" || strings.TrimSpace(req.SSHKey) == "" {
		return errors.New("user and ssh_key required")
	}
	if !strings.Contains(req.SSHKey, "-----BEGIN OPENSSH PRIVATE KEY-----") || !strings.Contains(req.SSHKey, "-----END OPENSSH PRIVATE KEY-----") {
		return errors.New("ssh_key must be a valid OPENSSH private key")
	}
	return nil
}

func (req installDefaultsRequest) toInstallConfig() db.InstallConfig {
	return db.InstallConfig{
		User:   strings.TrimSpace(req.User),
		SSHKey: req.SSHKey,
	}
}

func (req installConfigRequest) toInstallConfig() db.InstallConfig {
	return db.InstallConfig{
		Address: strings.TrimSpace(req.Address),
		User:    strings.TrimSpace(req.User),
		SSHKey:  req.SSHKey,
	}
}
