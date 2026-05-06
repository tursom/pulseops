package task

import (
	"context"
	"fmt"
	"net"

	"pulseops/internal/config"
)

type TCPCheckParams struct {
	Address string `json:"address"`
}

type TCPCheckDriver struct{}

func (TCPCheckDriver) Kind() string {
	return "tcp_check"
}

func (TCPCheckDriver) Validate(spec config.TaskSpec) error {
	params, err := DecodeParams[TCPCheckParams](spec.Params)
	if err != nil {
		return err
	}
	if params.Address == "" {
		return fmt.Errorf("tcp_check requires params.address")
	}
	return nil
}

func (TCPCheckDriver) NewRunner(spec config.TaskSpec, _ RunnerDeps) (Runner, error) {
	params, err := DecodeParams[TCPCheckParams](spec.Params)
	if err != nil {
		return nil, err
	}
	return &tcpCheckRunner{params: params}, nil
}

type tcpCheckRunner struct {
	params TCPCheckParams
}

func (r *tcpCheckRunner) Run(ctx context.Context, _ TriggerType) (Result, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", r.params.Address)
	if err != nil {
		return Result{}, fmt.Errorf("dial tcp: %w", err)
	}
	_ = conn.Close()
	return Result{
		CheckStatus: "pass",
		Summary: map[string]any{
			"address": r.params.Address,
		},
	}, nil
}
