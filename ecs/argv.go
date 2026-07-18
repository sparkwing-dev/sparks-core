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

// dryRunInputRef and dryRunTaskDefRef are stand-in tokens for the two
// values a real run derives from live state (the generated task-def file
// and the freshly registered ARN), so the echoed dry-run argv reads
// clearly without contacting AWS.
const (
	dryRunInputRef   = "file://<generated-task-def.json>"
	dryRunTaskDefRef = "<new-task-def-arn>"
)

// regionProfileArgs returns the trailing --region/--profile flags common
// to every aws call: --region when Region is set, then the profile args
// (which collapse to nothing under IRSA). See aws.ProfileArgs.
func regionProfileArgs(region, awsProfile string) []string {
	var a []string
	if region != "" {
		a = append(a, "--region", region)
	}
	return append(a, aws.ProfileArgs(awsProfile)...)
}

// describeArgs builds the argv for reading a family's current task
// definition as JSON.
func describeArgs(taskFamily string, rp []string) []string {
	args := []string{"ecs", "describe-task-definition",
		"--task-definition", taskFamily,
		"--query", "taskDefinition",
		"--output", "json"}
	return append(args, rp...)
}

// registerArgs builds the argv for registering a new revision from a
// cli-input-json reference (a file://... path, or a placeholder under
// dry-run), returning the new revision's ARN as plain text.
func registerArgs(inputRef string, rp []string) []string {
	args := []string{"ecs", "register-task-definition",
		"--cli-input-json", inputRef,
		"--query", "taskDefinition.taskDefinitionArn",
		"--output", "text"}
	return append(args, rp...)
}

// updateServiceArgs builds the argv for pointing a service at a task
// definition (an ARN or family:revision).
func updateServiceArgs(cluster, service, taskDef string, rp []string) []string {
	args := []string{"ecs", "update-service",
		"--cluster", cluster,
		"--service", service,
		"--task-definition", taskDef}
	return append(args, rp...)
}

// waitStableArgs builds the argv for blocking until a service reaches a
// steady state.
func waitStableArgs(cluster, service string, rp []string) []string {
	args := []string{"ecs", "wait", "services-stable",
		"--cluster", cluster,
		"--services", service}
	return append(args, rp...)
}

// echoArgv logs the aws command a dry run would have executed.
func echoArgv(ctx context.Context, args []string) {
	sparkwing.Info(ctx, "[dry-run] aws %s", strings.Join(args, " "))
}

// buildRegisterInput turns a describe-task-definition JSON object into a
// register-task-definition input: it swaps the named container's image,
// strips the fields register rejects, and returns the re-marshaled input
// plus the prior task-definition ARN read off the source.
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

// writeTaskDefFile writes the register input to a temp file and returns
// its path; the caller is responsible for removing it.
func writeTaskDefFile(input []byte) (string, error) {
	f, err := os.CreateTemp("", "ecs-taskdef-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(input); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
