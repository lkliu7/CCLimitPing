package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const processCheckTimeout = 3 * time.Second

type processInfo struct {
	pid  int
	comm string
	args string
}

func activeCLIProcess(ctx context.Context, names, markers []string) (string, bool, error) {
	if runtime.GOOS == "windows" {
		return "", false, nil
	}

	pctx, cancel := context.WithTimeout(ctx, processCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(pctx, "ps", "-axo", "pid=,comm=,args=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", false, fmt.Errorf("ps: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	ownPID := os.Getpid()
	for _, line := range strings.Split(stdout.String(), "\n") {
		proc, ok := parseProcessLine(line)
		if !ok || proc.pid == ownPID || processBase(proc.comm) == "limitping" || processBase(proc.comm) == "ps" {
			continue
		}
		if processMatches(proc, names, markers) {
			return fmt.Sprintf("%s(pid %d)", processBase(proc.comm), proc.pid), true, nil
		}
	}
	return "", false, nil
}

func parseProcessLine(line string) (processInfo, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return processInfo{}, false
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil {
		return processInfo{}, false
	}
	args := ""
	if len(fields) > 2 {
		args = strings.Join(fields[2:], " ")
	}
	return processInfo{pid: pid, comm: fields[1], args: args}, true
}

func processMatches(proc processInfo, names, markers []string) bool {
	comm := processBase(proc.comm)
	if stringIn(comm, names) {
		return true
	}
	fields := strings.Fields(proc.args)
	if len(fields) > 0 && stringIn(processBase(fields[0]), names) {
		return true
	}

	args := strings.ToLower(proc.args)
	for _, marker := range markers {
		if marker != "" && strings.Contains(args, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func processBase(s string) string {
	base := filepath.Base(s)
	if runtimeExt := filepath.Ext(base); runtimeExt == ".exe" {
		base = strings.TrimSuffix(base, runtimeExt)
	}
	return strings.ToLower(base)
}

func stringIn(s string, values []string) bool {
	for _, v := range values {
		if strings.EqualFold(s, v) {
			return true
		}
	}
	return false
}
