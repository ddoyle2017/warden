package service

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	vanilla = "vanilla"
	modded  = "modded"

	vanillaServerScript = "start_server.sh"
	moddedServerScript  = "start_server_bepinex.sh"
)

var (
	ErrInvalidGameType   = errors.New("invalid game server type")
	ErrServerStartFailed = errors.New("unable to start game server")
)

// Exposes all methods for interacting with the Valheim game server.
type Server interface {
	Start(config string) (string, error)
	IsValidGameType(config string) bool
}

type serverService struct {
	valheimDirectory string
}

func NewServerService(valheimDirectory string) Server {
	return &serverService{
		valheimDirectory: valheimDirectory,
	}
}

func (s *serverService) Start(config string) (string, error) {
	config = normalize(config)

	var scriptPath string
	if config == modded {
		scriptPath = filepath.Join(s.valheimDirectory, moddedServerScript)
	} else if config == vanilla {
		scriptPath = filepath.Join(s.valheimDirectory, vanillaServerScript)
	} else {
		return "", ErrInvalidGameType
	}

	cmd := exec.Command("sh", scriptPath)

	// Capture the output of the script
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), ErrServerStartFailed
	}
	return string(output), nil
}

func (s *serverService) IsValidGameType(config string) bool {
	config = normalize(config)
	return config == vanilla || config == modded
}

func normalize(s string) string {
	s = strings.ToLower(s)
	return strings.TrimSpace(s)
}
