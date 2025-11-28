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
	return nil
}

func (req installConfigRequest) toInstallConfig() db.InstallConfig {
	return db.InstallConfig{
		Address: strings.TrimSpace(req.Address),
		User:    strings.TrimSpace(req.User),
		SSHKey:  req.SSHKey,
	}
}
