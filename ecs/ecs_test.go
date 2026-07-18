package ecs

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func awsEnvOff(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv(dryRunEnv, "")
}

func TestRegionProfileArgs_RegionAndProfile(t *testing.T) {
	awsEnvOff(t)
	got := regionProfileArgs("us-west-2", "ci")
	want := []string{"--region", "us-west-2", "--profile", "ci"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("regionProfileArgs = %v, want %v", got, want)
	}
}

func TestRegionProfileArgs_EmptyRegionOmitsFlag(t *testing.T) {
	awsEnvOff(t)
	got := regionProfileArgs("", "ci")
	want := []string{"--profile", "ci"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("regionProfileArgs = %v, want %v", got, want)
	}
}

func TestRegionProfileArgs_IRSADropsProfile(t *testing.T) {
	awsEnvOff(t)
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "/var/run/token")
	got := regionProfileArgs("us-west-2", "ci")
	want := []string{"--region", "us-west-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("regionProfileArgs under IRSA = %v, want %v", got, want)
	}
}

func TestDescribeArgs(t *testing.T) {
	got := describeArgs("myapp", []string{"--region", "us-west-2"})
	want := []string{"ecs", "describe-task-definition",
		"--task-definition", "myapp",
		"--query", "taskDefinition",
		"--output", "json",
		"--region", "us-west-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("describeArgs = %v, want %v", got, want)
	}
}

func TestRegisterArgs(t *testing.T) {
	got := registerArgs("file:///tmp/td.json", nil)
	want := []string{"ecs", "register-task-definition",
		"--cli-input-json", "file:///tmp/td.json",
		"--query", "taskDefinition.taskDefinitionArn",
		"--output", "text"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("registerArgs = %v, want %v", got, want)
	}
}

func TestUpdateServiceArgs(t *testing.T) {
	got := updateServiceArgs("prod", "web", "arn:aws:ecs:...:task-definition/web:7", []string{"--profile", "ci"})
	want := []string{"ecs", "update-service",
		"--cluster", "prod",
		"--service", "web",
		"--task-definition", "arn:aws:ecs:...:task-definition/web:7",
		"--profile", "ci"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updateServiceArgs = %v, want %v", got, want)
	}
}

func TestWaitStableArgs(t *testing.T) {
	got := waitStableArgs("prod", "web", nil)
	want := []string{"ecs", "wait", "services-stable",
		"--cluster", "prod",
		"--services", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("waitStableArgs = %v, want %v", got, want)
	}
}

const sampleTaskDef = `{
  "taskDefinitionArn": "arn:aws:ecs:us-west-2:123:task-definition/web:6",
  "family": "web",
  "revision": 6,
  "status": "ACTIVE",
  "requiresAttributes": [{"name": "ecs.capability.execution-role-awslogs"}],
  "compatibilities": ["EC2", "FARGATE"],
  "registeredAt": "2026-01-01T00:00:00Z",
  "registeredBy": "arn:aws:iam::123:root",
  "cpu": "256",
  "memory": "512",
  "networkMode": "awsvpc",
  "executionRoleArn": "arn:aws:iam::123:role/exec",
  "containerDefinitions": [
    {"name": "web", "image": "123.dkr.ecr/web:old", "essential": true},
    {"name": "sidecar", "image": "123.dkr.ecr/sidecar:pinned"}
  ]
}`

func TestBuildRegisterInput_SwapsImageStripsReadOnly(t *testing.T) {
	input, prev, err := buildRegisterInput([]byte(sampleTaskDef), "web", "123.dkr.ecr/web:new")
	if err != nil {
		t.Fatalf("buildRegisterInput error: %v", err)
	}
	if prev != "arn:aws:ecs:us-west-2:123:task-definition/web:6" {
		t.Fatalf("prev = %q, want the source ARN", prev)
	}
	var def map[string]any
	if err := json.Unmarshal(input, &def); err != nil {
		t.Fatalf("register input is not valid JSON: %v", err)
	}
	for _, k := range registerReadOnlyKeys {
		if _, ok := def[k]; ok {
			t.Errorf("register input still carries read-only key %q", k)
		}
	}
	if def["family"] != "web" {
		t.Errorf("family not preserved: %v", def["family"])
	}
	containers, _ := def["containerDefinitions"].([]any)
	if len(containers) != 2 {
		t.Fatalf("containerDefinitions = %d entries, want 2", len(containers))
	}
	web := containers[0].(map[string]any)
	if web["image"] != "123.dkr.ecr/web:new" {
		t.Errorf("web image = %v, want swapped", web["image"])
	}
	sidecar := containers[1].(map[string]any)
	if sidecar["image"] != "123.dkr.ecr/sidecar:pinned" {
		t.Errorf("sidecar image = %v, should be untouched", sidecar["image"])
	}
}

func TestBuildRegisterInput_UnknownContainer(t *testing.T) {
	_, _, err := buildRegisterInput([]byte(sampleTaskDef), "does-not-exist", "img:new")
	if err == nil {
		t.Fatal("expected an error for an unknown container name")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("error should name the missing container, got: %v", err)
	}
}

func TestBuildRegisterInput_InvalidJSON(t *testing.T) {
	if _, _, err := buildRegisterInput([]byte("not json"), "web", "img:new"); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestBuildRegisterInput_NoContainerDefinitions(t *testing.T) {
	if _, _, err := buildRegisterInput([]byte(`{"family":"web"}`), "web", "img:new"); err == nil {
		t.Fatal("expected an error when containerDefinitions is absent")
	}
}

func TestDeploy_ValidatesRequiredFields(t *testing.T) {
	awsEnvOff(t)
	_, err := Deploy(context.Background(), DeployConfig{Cluster: "prod"})
	if err == nil {
		t.Fatal("expected a validation error for a mostly-empty config")
	}
	for _, field := range []string{"Service", "TaskFamily", "ContainerName", "Image"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("validation error should mention %s, got: %v", field, err)
		}
	}
}

func TestDeploy_DryRunSkipsExecAndReturnsEmptyPrior(t *testing.T) {
	awsEnvOff(t)
	prev, err := Deploy(context.Background(), DeployConfig{
		Cluster:       "prod",
		Service:       "web",
		TaskFamily:    "web",
		ContainerName: "web",
		Image:         "123.dkr.ecr/web:new",
		Region:        "us-west-2",
		AWSProfile:    "ci",
		DryRun:        true,
	})
	if err != nil {
		t.Fatalf("dry-run Deploy should succeed without AWS, got: %v", err)
	}
	if prev != "" {
		t.Fatalf("dry-run Deploy prior ARN = %q, want empty", prev)
	}
}

func TestDeploy_DryRunViaEnv(t *testing.T) {
	awsEnvOff(t)
	t.Setenv(dryRunEnv, "1")
	if _, err := Deploy(context.Background(), DeployConfig{
		Cluster:       "prod",
		Service:       "web",
		TaskFamily:    "web",
		ContainerName: "web",
		Image:         "img:new",
	}); err != nil {
		t.Fatalf("SPARKWING_DRY_RUN Deploy should succeed, got: %v", err)
	}
}

func TestRollback_ValidatesRequiredFields(t *testing.T) {
	awsEnvOff(t)
	err := Rollback(context.Background(), RollbackConfig{Cluster: "prod"})
	if err == nil {
		t.Fatal("expected a validation error")
	}
	for _, field := range []string{"Service", "TaskDefinition"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("validation error should mention %s, got: %v", field, err)
		}
	}
}

func TestRollback_DryRunSkipsExec(t *testing.T) {
	awsEnvOff(t)
	t.Setenv(dryRunEnv, "1")
	if err := Rollback(context.Background(), RollbackConfig{
		Cluster:        "prod",
		Service:        "web",
		TaskDefinition: "arn:aws:ecs:us-west-2:123:task-definition/web:6",
		Region:         "us-west-2",
		AWSProfile:     "ci",
	}); err != nil {
		t.Fatalf("dry-run Rollback should succeed without AWS, got: %v", err)
	}
}
