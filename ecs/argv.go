package ecs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sparkwing-dev/sparkwing/sparkwing"

	"github.com/sparkwing-dev/sparks-core/aws"
)

// Stand-in tokens for the two values a real run derives from live state.
const (
	dryRunInputRef   = "file://<generated-task-def.json>"
	dryRunTaskDefRef = "<new-task-def-arn>"
)

func regionProfileArgs(region, awsProfile string) []string {
	var a []string
	if region != "" {
		a = append(a, "--region", region)
	}
	return append(a, aws.ProfileArgs(awsProfile)...)
}

func describeArgs(taskFamily string, rp []string) []string {
	args := []string{
		"ecs", "describe-task-definition",
		"--task-definition", taskFamily,
		"--query", "taskDefinition",
		"--output", "json",
	}
	return append(args, rp...)
}

func registerArgs(inputRef string, extra, rp []string) []string {
	args := []string{
		"ecs", "register-task-definition",
		"--cli-input-json", inputRef,
		"--query", "taskDefinition.taskDefinitionArn",
		"--output", "text",
	}
	args = append(args, extra...)
	return append(args, rp...)
}

func updateServiceArgs(cluster, service, taskDef string, extra, rp []string) []string {
	args := []string{
		"ecs", "update-service",
		"--cluster", cluster,
		"--service", service,
		"--task-definition", taskDef,
	}
	args = append(args, extra...)
	return append(args, rp...)
}

func waitStableArgs(cluster, service string, rp []string) []string {
	args := []string{
		"ecs", "wait", "services-stable",
		"--cluster", cluster,
		"--services", service,
	}
	return append(args, rp...)
}

func describeServicesArgs(cluster, service string, rp []string) []string {
	args := []string{
		"ecs", "describe-services",
		"--cluster", cluster,
		"--services", service,
		"--query", "services[0]",
		"--output", "json",
	}
	return append(args, rp...)
}

func echoArgv(ctx context.Context, args []string) {
	sparkwing.Info(ctx, "[dry-run] aws %s", strings.Join(args, " "))
}

func buildRegisterInput(describeJSON []byte, containerName, image string) (input []byte, prevTaskDef string, err error) {
	var def map[string]any
	if err := json.Unmarshal(describeJSON, &def); err != nil {
		return nil, "", fmt.Errorf("ecs: parse task definition: %w", err)
	}
	if arn, ok := def["taskDefinitionArn"].(string); ok {
		prevTaskDef = arn
	}
	containers, ok := def["containerDefinitions"].([]any)
	if !ok {
		return nil, "", fmt.Errorf("ecs: task definition has no containerDefinitions")
	}
	swapped := false
	for _, c := range containers {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := cm["name"].(string); name == containerName {
			cm["image"] = image
			swapped = true
		}
	}
	if !swapped {
		return nil, "", fmt.Errorf("ecs: container %q not found in task definition %s", containerName, prevTaskDef)
	}
	for _, k := range registerReadOnlyKeys {
		delete(def, k)
	}
	input, err = json.Marshal(def)
	if err != nil {
		return nil, "", fmt.Errorf("ecs: marshal register input: %w", err)
	}
	return input, prevTaskDef, nil
}

// serviceStable mirrors the aws `wait services-stable` success condition:
// one deployment, COMPLETED or with no rolloutState, and runningCount equal
// to desiredCount. A FAILED rollout errors so the poll stops early.
func serviceStable(servicesJSON []byte) (bool, error) {
	var svc struct {
		RunningCount int `json:"runningCount"`
		DesiredCount int `json:"desiredCount"`
		Deployments  []struct {
			Status       string `json:"status"`
			RolloutState string `json:"rolloutState"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(servicesJSON, &svc); err != nil {
		return false, fmt.Errorf("ecs: parse describe-services output: %w", err)
	}
	for _, d := range svc.Deployments {
		if d.RolloutState == "FAILED" {
			return false, fmt.Errorf("ecs: %s deployment rollout failed", d.Status)
		}
	}
	if len(svc.Deployments) != 1 {
		return false, nil
	}
	primary := svc.Deployments[0]
	if primary.RolloutState != "" && primary.RolloutState != "COMPLETED" {
		return false, nil
	}
	return svc.RunningCount == svc.DesiredCount, nil
}

// writeTaskDefFile returns a path the caller must remove.
func writeTaskDefFile(input []byte) (string, error) {
	f, err := os.CreateTemp("", "ecs-taskdef-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(input); err != nil {
		f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
