package task

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"pulseops/internal/config"
)

type ProcessCheckParams struct {
	Name string `json:"name"`
}

type ProcessCheckDriver struct{}

func (ProcessCheckDriver) Kind() string {
	return "process_check"
}

func (ProcessCheckDriver) Validate(spec config.TaskSpec) error {
	params, err := DecodeParams[ProcessCheckParams](spec.Params)
	if err != nil {
		return err
	}
	if params.Name == "" {
		return fmt.Errorf("process_check requires params.name")
	}
	return nil
}

func (ProcessCheckDriver) NewRunner(spec config.TaskSpec, _ RunnerDeps) (Runner, error) {
	params, err := DecodeParams[ProcessCheckParams](spec.Params)
	if err != nil {
		return nil, err
	}
	return &processCheckRunner{params: params}, nil
}

type processCheckRunner struct {
	params ProcessCheckParams
}

func (r *processCheckRunner) Run(_ context.Context, _ TriggerType) (Result, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return Result{}, fmt.Errorf("read /proc: %w", err)
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), r.params.Name) {
			count++
		}
	}
	checkStatus := "fail"
	if count > 0 {
		checkStatus = "pass"
	}
	return Result{
		CheckStatus: checkStatus,
		Summary: map[string]any{
			"process_count": count,
		},
	}, nil
}
