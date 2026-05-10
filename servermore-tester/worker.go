package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type RequestSettings struct {
	BatchSize           int
	RequestsPerSecond   int
	DelayBetweenBatches int
}

type RunStats struct {
	RequestsSent      int64
	ResponsesReceived int64
	TransportErrors   int64
	BatchesCompleted  int64
	LastStatusCode    int
	LastMethod        string
	LastPath          string
	LastDuration      time.Duration
	LastResponse      string
	LastError         string
	LastUpdatedAt     time.Time
}

type DeploymentSnapshot struct {
	Name       string
	FunctionID string
	Settings   RequestSettings
	Stats      RunStats
}

type deploymentState struct {
	mu sync.RWMutex

	name       string
	functionID string
	settings   RequestSettings
	stats      RunStats
	cancel     context.CancelFunc
}

func newDeploymentState(
	name string,
	functionID string,
	cancel context.CancelFunc,
) *deploymentState {
	return &deploymentState{
		name:       name,
		functionID: functionID,
		settings: RequestSettings{
			BatchSize:           3,
			RequestsPerSecond:   2,
			DelayBetweenBatches: 2,
		},
		cancel: cancel,
	}
}

func (d *deploymentState) Snapshot() DeploymentSnapshot {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return DeploymentSnapshot{
		Name:       d.name,
		FunctionID: d.functionID,
		Settings:   d.settings,
		Stats:      d.stats,
	}
}

func (d *deploymentState) SettingsSnapshot() RequestSettings {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.settings
}

func (d *deploymentState) AdjustSetting(fieldIndex int, delta int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch fieldIndex {
	case 0:
		d.settings.BatchSize = maxInt(0, d.settings.BatchSize+delta)
	case 1:
		d.settings.RequestsPerSecond = maxInt(0, d.settings.RequestsPerSecond+delta)
	case 2:
		d.settings.DelayBetweenBatches = maxInt(0, d.settings.DelayBetweenBatches+delta)
	}
}

func (d *deploymentState) RecordRequest(
	spec InvocationSpec,
	statusCode int,
	response string,
	err error,
	took time.Duration,
) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stats.RequestsSent++
	d.stats.LastMethod = ensureMethod(spec.Method)
	d.stats.LastPath = spec.Path
	d.stats.LastDuration = took
	d.stats.LastUpdatedAt = time.Now()

	if err != nil {
		d.stats.TransportErrors++
		d.stats.LastError = compactText(err.Error(), 140)
		d.stats.LastStatusCode = 0
		d.stats.LastResponse = ""
		return
	}

	d.stats.ResponsesReceived++
	d.stats.LastStatusCode = statusCode
	d.stats.LastError = ""
	d.stats.LastResponse = compactText(response, 140)
}

func (d *deploymentState) RecordBatchCompletion() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stats.BatchesCompleted++
	d.stats.LastUpdatedAt = time.Now()
}

func (d *deploymentState) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

func startWorker(
	ctx context.Context,
	wg *sync.WaitGroup,
	requester Requester,
	deployment *deploymentState,
) {
	wg.Go(func() {
		runWorkerLoop(ctx, requester, deployment)
	})
}

func runWorkerLoop(ctx context.Context, requester Requester, deployment *deploymentState) {
	client := &http.Client{Timeout: 15 * time.Second}

	for {
		settings := deployment.SettingsSnapshot()
		if settings.BatchSize <= 0 || settings.RequestsPerSecond <= 0 {
			if !sleepWithContext(ctx, 250*time.Millisecond) {
				return
			}
			continue
		}

		sentInBatch := 0
		for {
			settings = deployment.SettingsSnapshot()
			if settings.BatchSize <= 0 || settings.RequestsPerSecond <= 0 ||
				sentInBatch >= settings.BatchSize {
				break
			}

			spec := requester.NextInvocation()
			startedAt := time.Now()
			statusCode, responsePreview, err := invokeFunction(
				ctx,
				client,
				deployment.functionID,
				spec,
			)
			deployment.RecordRequest(spec, statusCode, responsePreview, err, time.Since(startedAt))
			sentInBatch++

			nextSettings := deployment.SettingsSnapshot()
			if nextSettings.BatchSize <= 0 || nextSettings.RequestsPerSecond <= 0 ||
				sentInBatch >= nextSettings.BatchSize {
				continue
			}

			interval := time.Second / time.Duration(maxInt(1, nextSettings.RequestsPerSecond))
			if !sleepWithContext(ctx, interval) {
				return
			}
		}

		if sentInBatch > 0 {
			deployment.RecordBatchCompletion()
		}

		delay := time.Duration(deployment.SettingsSnapshot().DelayBetweenBatches) * time.Second
		if delay > 0 && !sleepWithContext(ctx, delay) {
			return
		}
	}
}

func invokeFunction(
	ctx context.Context,
	client *http.Client,
	functionID string,
	spec InvocationSpec,
) (int, string, error) {
	path := spec.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	url := strings.TrimRight(gatewayURL, "/") + "/" + functionID + path
	req, err := http.NewRequestWithContext(
		requestCtx,
		ensureMethod(spec.Method),
		url,
		bytes.NewReader(spec.Body),
	)
	if err != nil {
		return 0, "", fmt.Errorf("build gateway request: %w", err)
	}
	for key, value := range spec.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("User-Agent", "servermore-tester")

	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("invoke %s %s: %w", req.Method, path, err)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return 0, "", fmt.Errorf("read response body: %w", readErr)
	}

	return resp.StatusCode, string(bodyBytes), nil
}

func sleepWithContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return true
	}

	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
