package evals

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type ProcessClientFactoryConfig struct {
	Output      io.Writer
	Environment map[string]string
	Binary      string
	DataDir     string
	Transport   string
	Entrypoint  string
	AppName     string
	Source      SourceEvidence
	Timeout     time.Duration
	Streaming   bool
}

func NewProcessClientFactory(ctx context.Context, config ProcessClientFactoryConfig) (ClientFactory, error) {
	return newProcessClientFactory(ctx, config, executeRuntimeCommand)
}

func newProcessClientFactory(
	ctx context.Context, config ProcessClientFactoryConfig, run runtimeCommand,
) (ClientFactory, error) {
	if config.Binary == "" || config.DataDir == "" {
		return nil, errors.New("process client factory needs the agent binary and data directory")
	}
	if config.Transport != "rest" && config.Transport != "a2a" {
		return nil, fmt.Errorf("unsupported process client transport %q", config.Transport)
	}
	if config.Transport == "a2a" && config.Entrypoint != "agent" {
		return nil, errors.New("the deployed A2A contract serves only the agent entrypoint")
	}
	if err := verifyRuntimeIdentity(ctx, config.Binary, config.Source, run); err != nil {
		return nil, err
	}
	return func(ctx context.Context, evalCase EvalCase, _ int) (AgentClient, func() error, error) {
		stateDir, removeState, err := NewIsolatedState()
		if err != nil {
			return nil, nil, err
		}
		binary, err := snapshotRuntimeBinary(config.Binary, stateDir)
		if err != nil {
			_ = removeState()
			return nil, nil, err
		}
		if identityErr := verifyRuntimeIdentity(ctx, binary, config.Source, run); identityErr != nil {
			_ = removeState()
			return nil, nil, identityErr
		}
		port, err := FreePort()
		if err != nil {
			_ = removeState()
			return nil, nil, err
		}
		process, err := StartAgentProcess(ctx, AgentProcessConfig{
			Binary: binary, Transport: config.Transport, Entrypoint: config.Entrypoint,
			DataDir: config.DataDir, StateDir: stateDir, Port: port,
			Environment: config.Environment, Output: config.Output,
		})
		if err != nil {
			_ = removeState()
			return nil, nil, err
		}
		cleanup := func() error { return errors.Join(process.Close(), removeState()) }
		if config.Transport == "a2a" {
			a2aClient, clientErr := NewA2AClient(A2AClientConfig{BaseURL: process.BaseURL(), Timeout: config.Timeout})
			if clientErr != nil {
				_ = cleanup()
				return nil, nil, clientErr
			}
			return a2aClient, cleanup, nil
		}
		appName := config.AppName
		if appName == "" {
			appName = evalCase.SessionInput.AppName
		}
		client, err := NewRESTClient(RESTClientConfig{
			BaseURL: process.BaseURL(), AppName: appName, UserID: evalCase.SessionInput.UserID,
			Streaming: config.Streaming, Timeout: config.Timeout,
		})
		if err != nil {
			_ = cleanup()
			return nil, nil, err
		}
		return client, cleanup, nil
	}, nil
}
