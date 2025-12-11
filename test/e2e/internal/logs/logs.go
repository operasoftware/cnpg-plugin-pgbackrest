/*
Copyright 2025, Opera Norway AS

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// GetPodContainerLogs retrieves logs from a specific container in a pod and parses them as JSON.
// All logs are expected to be valid structured JSON. Returns an error if any line is not valid JSON.
func GetPodContainerLogs(
	ctx context.Context,
	clientSet *kubernetes.Clientset,
	namespace, podName, containerName string,
	sinceSeconds *int64,
) ([]map[string]any, error) {
	podLogOpts := corev1.PodLogOptions{
		Container: containerName,
	}
	if sinceSeconds != nil {
		podLogOpts.SinceSeconds = sinceSeconds
	}

	req := clientSet.CoreV1().Pods(namespace).GetLogs(podName, &podLogOpts)
	stream, err := req.Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("error opening log stream: %w", err)
	}
	defer stream.Close()

	var logEntries []map[string]any
	scanner := bufio.NewScanner(stream)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Check if line is JSON
		if !strings.HasPrefix(line, "{") {
			return nil, fmt.Errorf("non-JSON line at line %d: %s", lineNum, line)
		}

		var logEntry map[string]any
		if err := json.Unmarshal([]byte(line), &logEntry); err != nil {
			return nil, fmt.Errorf("invalid JSON at line %d: %w", lineNum, err)
		}

		logEntries = append(logEntries, logEntry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading logs: %w", err)
	}

	return logEntries, nil
}

// FindArchiveBatches finds "WAL archive batch prepared" log entries and returns the parsed data.
// Each returned map contains the structured log fields.
func FindArchiveBatches(logEntries []map[string]any) []map[string]any {
	var batches []map[string]any

	for _, logEntry := range logEntries {
		// Check if this is a "WAL archive batch prepared" message
		if msg, ok := logEntry["msg"].(string); ok && msg == "WAL archive batch prepared" {
			batches = append(batches, logEntry)
		}
	}

	return batches
}

// FindArchiveBatchCompletions finds "WAL archive batch completed" log entries and returns the parsed data.
// Each returned map contains the structured log fields.
func FindArchiveBatchCompletions(logEntries []map[string]any) []map[string]any {
	var batches []map[string]any

	for _, logEntry := range logEntries {
		// Check if this is a "WAL archive batch completed" message
		if msg, ok := logEntry["msg"].(string); ok && msg == "WAL archive batch completed" {
			batches = append(batches, logEntry)
		}
	}

	return batches
}
