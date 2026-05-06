package task

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"pulseops/internal/config"
)

type ScriptExecParams struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	WorkDir string            `json:"workdir"`
	Env     map[string]string `json:"env"`
}

type ScriptExecDriver struct{}

func (ScriptExecDriver) Kind() string {
	return "script_exec"
}

func (ScriptExecDriver) Validate(spec config.TaskSpec) error {
	params, err := DecodeParams[ScriptExecParams](spec.Params)
	if err != nil {
		return err
	}
	if params.Command == "" {
		return fmt.Errorf("script_exec requires params.command")
	}
	return nil
}

func (ScriptExecDriver) NewRunner(spec config.TaskSpec, deps RunnerDeps) (Runner, error) {
	params, err := DecodeParams[ScriptExecParams](spec.Params)
	if err != nil {
		return nil, err
	}
	return &scriptExecRunner{params: params, baseDir: deps.BaseDir}, nil
}

type scriptExecRunner struct {
	params  ScriptExecParams
	baseDir string
}

func (r *scriptExecRunner) Run(ctx context.Context, _ TriggerType) (Result, error) {
	cmd := exec.CommandContext(ctx, r.params.Command, r.params.Args...)
	if r.params.WorkDir != "" {
		cmd.Dir = config.ResolvePath(r.baseDir, r.params.WorkDir)
	}
	cmd.Env = os.Environ()
	for key, value := range r.params.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return Result{
			CheckStatus: "pass",
			Summary: map[string]any{
				"exit_code": 0,
			},
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return Result{
			CheckStatus: "fail",
			Summary: map[string]any{
				"exit_code": exitErr.ExitCode(),
			},
			Stdout: stdout.String(),
			Stderr: stderr.String(),
		}, nil
	}
	return Result{}, fmt.Errorf("run script: %w", err)
}
