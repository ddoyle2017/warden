package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"warden/internal/config"
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
	fmt.Printf("Launching %s game server...\n\n", gameType)

	cmd, stdout, stderr, err := startCommand("sh", s.getStartScript(gameType))
	if err != nil {
		return "", ErrServerStartFailed
	}

	streamOutput(stdout, stderr)

	// Since command runs in the background, wait for it to finish
	if err := cmd.Wait(); err != nil {
		fmt.Println("... Server crashed!")
	} else {
		fmt.Println("... Closing server!")
	}
	return "", nil
}

func (s *serverService) IsValidGameType(config string) bool {
	config = normalize(config)
	return config == vanilla || config == modded
}

func (s *serverService) getStartScript(gameType string) string {
	if gameType == modded {
		return filepath.Join(s.ValheimDirectory, moddedStartScript)
	} else {
		switch s.Platform {
		case config.Linux:
			return filepath.Join(s.ValheimDirectory, linuxStartScript)
		case config.MacOS:
			return filepath.Join(s.ValheimDirectory, macOSStartScript)
		case config.Windows:
			return filepath.Join(s.ValheimDirectory, windowsStartScript)
		default:
			panic("Unrecognized operating system")
		}
	}
}

func normalize(s string) string {
	s = strings.ToLower(s)
	return strings.TrimSpace(s)
}

func startCommand(name string, args ...string) (*exec.Cmd, io.ReadCloser, io.ReadCloser, error) {
	// Initialize the command for running the server script +
	// setup output pipes for streaming logs to user.
	cmd := exec.Command(name, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, errors.New("unable to create stdout pipe")
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, errors.New("unable to create stderr pipe")
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, ErrServerStartFailed
	}
	return cmd, stdoutPipe, stderrPipe, nil
}

// Streams output from both stdout and stderr to the command line
func streamOutput(stdout, stderr io.Reader) {
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return
		}
	}()
}
