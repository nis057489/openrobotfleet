package controller

import (
	"errors"
	"strings"

	"example.com/turtlebot-fleet/internal/db"
)

type installConfigRequest struct {
	Address  string `json:"address"`
	User     string `json:"user"`
	SSHKey   string `json:"ssh_key"`
	Password string `json:"password"`
}

func (req installConfigRequest) validate() error {
	if strings.TrimSpace(req.Address) == "" || strings.TrimSpace(req.User) == "" {
		return errors.New("address and user required")
	}
	if strings.TrimSpace(req.SSHKey) == "" && strings.TrimSpace(req.Password) == "" {
		return errors.New("ssh_key or password required")
	}
	if strings.TrimSpace(req.SSHKey) != "" {
		if !strings.Contains(req.SSHKey, "-----BEGIN OPENSSH PRIVATE KEY-----") || !strings.Contains(req.SSHKey, "-----END OPENSSH PRIVATE KEY-----") {
			return errors.New("ssh_key must be a valid OPENSSH private key")
		}
	}
	return nil
}

type installDefaultsRequest struct {
	User     string `json:"user"`
	SSHKey   string `json:"ssh_key"`
	Password string `json:"password"`
}

func (req installDefaultsRequest) validate() error {
	if strings.TrimSpace(req.User) == "" {
		return errors.New("user required")
	}
	if strings.TrimSpace(req.SSHKey) == "" && strings.TrimSpace(req.Password) == "" {
		return errors.New("ssh_key or password required")
	}
	if strings.TrimSpace(req.SSHKey) != "" {
		if !strings.Contains(req.SSHKey, "-----BEGIN OPENSSH PRIVATE KEY-----") || !strings.Contains(req.SSHKey, "-----END OPENSSH PRIVATE KEY-----") {
			return errors.New("ssh_key must be a valid OPENSSH private key")
		}
	}
	return nil
}

func (req installDefaultsRequest) toInstallConfig() db.InstallConfig {
	return db.InstallConfig{
		User:     strings.TrimSpace(req.User),
		SSHKey:   req.SSHKey,
		Password: req.Password,
	}
}

func (req installConfigRequest) toInstallConfig() db.InstallConfig {
	return db.InstallConfig{
		Address:  strings.TrimSpace(req.Address),
		User:     strings.TrimSpace(req.User),
		SSHKey:   req.SSHKey,
		Password: req.Password,
	}
}
