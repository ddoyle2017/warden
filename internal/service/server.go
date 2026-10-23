package service

import (
	"errors"
	"fmt"
	"github.com/ddoyle2017/warden/internal/config"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	vanilla = "vanilla"
	modded  = "modded"

	linuxStartScript   = "start_server.sh"
	macOSStartScript   = "start_server.command"
	windowsStartScript = "start_headless_server.bat"
	moddedStartScript  = "start_server_bepinex.sh"
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
	config.Config
}

func NewServerService(cfg config.Config) Server {
	return &serverService{
		cfg,
	}
}

func (s *serverService) Start(gameType string) (string, error) {
	gameType = normalize(gameType)
	if !s.IsValidGameType(gameType) {
		return "", ErrInvalidGameType
	}

	serverDirectory, err := filepath.Abs(s.ValheimDirectory)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrServerStartFailed, err)
	}

	cmd := exec.Command("sh", s.getStartScript(gameType, serverDirectory))
	cmd.Dir = serverDirectory

	// Capture the output of the script
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%w: %w", ErrServerStartFailed, err)
	}
	return string(output), nil
}

func (s *serverService) IsValidGameType(config string) bool {
	config = normalize(config)
	return config == vanilla || config == modded
}

func (s *serverService) getStartScript(gameType, serverDirectory string) string {
	if gameType == modded {
		return filepath.Join(serverDirectory, moddedStartScript)
	} else {
		switch s.Platform {
		case config.Linux:
			return filepath.Join(serverDirectory, linuxStartScript)
		case config.MacOS:
			return filepath.Join(serverDirectory, macOSStartScript)
		case config.Windows:
			return filepath.Join(serverDirectory, windowsStartScript)
		default:
			return ""
		}
	}
}

func normalize(s string) string {
	s = strings.ToLower(s)
	return strings.TrimSpace(s)
}
