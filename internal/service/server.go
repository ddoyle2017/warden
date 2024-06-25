package service

import (
	"errors"
	"os/exec"
	"path/filepath"
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

	var scriptPath string
	if config == modded {
		scriptPath = filepath.Join(s.valheimDirectory, moddedServerScript)
	} else {
		scriptPath = filepath.Join(s.valheimDirectory, vanillaServerScript)
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
	return config == vanilla || config == modded
}
