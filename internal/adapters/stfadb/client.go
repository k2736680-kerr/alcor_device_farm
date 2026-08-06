package stfadb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type commandRunner interface {
	Run(context.Context, string, ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, binary string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, binary, args...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", errors.Join(errors.New("ADB command failed"), err)
	}
	return strings.TrimSpace(stdout.String() + "\n" + stderr.String()), nil
}

type Config struct {
	Binary        string
	ServerAddress string
	Attempts      int
	RetryDelay    time.Duration
	runner        commandRunner
}

type Client struct {
	binary     string
	host       string
	port       string
	attempts   int
	retryDelay time.Duration
	runner     commandRunner
}

func New(config Config) (*Client, error) {
	config.Binary = strings.TrimSpace(config.Binary)
	if config.Binary == "" {
		config.Binary = "adb"
	}
	host, port, err := validEndpoint(config.ServerAddress)
	if err != nil {
		return nil, fmt.Errorf("STF ADB server address: %w", err)
	}
	if !isLoopback(host) {
		return nil, errors.New("STF ADB server must use a loopback address")
	}
	if config.Attempts <= 0 {
		config.Attempts = 3
	}
	if config.Attempts > 5 {
		return nil, errors.New("STF ADB attempts must not exceed 5")
	}
	if config.RetryDelay <= 0 {
		config.RetryDelay = 500 * time.Millisecond
	}
	if config.runner == nil {
		config.runner = execRunner{}
	}
	return &Client{binary: config.Binary, host: host, port: port, attempts: config.Attempts,
		retryDelay: config.RetryDelay, runner: config.runner}, nil
}

func (client *Client) Register(ctx context.Context, endpoint string) error {
	host, port, err := validEndpoint(endpoint)
	if err != nil {
		return fmt.Errorf("device ADB endpoint: %w", err)
	}
	endpoint = net.JoinHostPort(host, port)
	var lastErr error
	for attempt := 1; attempt <= client.attempts; attempt++ {
		output, runErr := client.runner.Run(ctx, client.binary, "-H", client.host, "-P", client.port, "connect", endpoint)
		if runErr == nil && strings.Contains(strings.ToLower(output), "connected to "+strings.ToLower(endpoint)) {
			return nil
		}
		if runErr == nil {
			runErr = errors.New("ADB connect returned an unexpected response")
		}
		lastErr = runErr
		if attempt == client.attempts {
			break
		}
		timer := time.NewTimer(client.retryDelay * time.Duration(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(errors.New("STF ADB registration context ended"), ctx.Err())
		case <-timer.C:
		}
	}
	return errors.Join(errors.New("STF ADB registration failed"), lastErr)
}

func validEndpoint(value string) (string, string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil || strings.TrimSpace(host) == "" {
		return "", "", errors.New("address must use host:port")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return "", "", errors.New("port must be between 1 and 65535")
	}
	return host, port, nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
