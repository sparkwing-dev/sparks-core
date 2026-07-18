package lambda

import (
	"context"
	"reflect"
	"testing"
)

var testProfile = []string{"--profile", "dev"}

func TestGetAliasArgs_QueriesFunctionVersion(t *testing.T) {
	got := getAliasArgs("checkout", "live", "us-west-2", testProfile)
	want := []string{
		"lambda", "get-alias",
		"--function-name", "checkout",
		"--name", "live",
		"--query", "FunctionVersion",
		"--output", "text",
		"--region", "us-west-2",
		"--profile", "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("getAliasArgs\n got %q\nwant %q", got, want)
	}
}

func TestGetAliasArgs_OmitsRegionWhenEmpty(t *testing.T) {
	got := getAliasArgs("checkout", "live", "", nil)
	want := []string{
		"lambda", "get-alias",
		"--function-name", "checkout",
		"--name", "live",
		"--query", "FunctionVersion",
		"--output", "text",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("getAliasArgs\n got %q\nwant %q", got, want)
	}
}

func TestUpdateImageCodeArgs_PublishesFromImageURI(t *testing.T) {
	got := updateImageCodeArgs("checkout", "123.dkr.ecr.us-west-2.amazonaws.com/checkout:abc", "us-west-2", nil, testProfile)
	want := []string{
		"lambda", "update-function-code",
		"--function-name", "checkout",
		"--image-uri", "123.dkr.ecr.us-west-2.amazonaws.com/checkout:abc",
		"--publish",
		"--query", "Version",
		"--output", "text",
		"--region", "us-west-2",
		"--profile", "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updateImageCodeArgs\n got %q\nwant %q", got, want)
	}
}

func TestUpdateImageCodeArgs_AppendsExtraArgs(t *testing.T) {
	got := updateImageCodeArgs("checkout", "uri", "", []string{"--architectures", "arm64"}, nil)
	want := []string{
		"lambda", "update-function-code",
		"--function-name", "checkout",
		"--image-uri", "uri",
		"--publish",
		"--query", "Version",
		"--output", "text",
		"--architectures", "arm64",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updateImageCodeArgs\n got %q\nwant %q", got, want)
	}
}

func TestUpdateZipS3Args_UpdatesFromStagedObject(t *testing.T) {
	got := updateZipS3Args("checkout", "artifacts", "checkout/function.zip", "eu-west-1", nil, testProfile)
	want := []string{
		"lambda", "update-function-code",
		"--function-name", "checkout",
		"--s3-bucket", "artifacts",
		"--s3-key", "checkout/function.zip",
		"--publish",
		"--query", "Version",
		"--output", "text",
		"--region", "eu-west-1",
		"--profile", "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updateZipS3Args\n got %q\nwant %q", got, want)
	}
}

func TestUpdateZipDirectArgs_UsesFileBPrefix(t *testing.T) {
	got := updateZipDirectArgs("checkout", "build/function.zip", "us-west-2", nil, nil)
	want := []string{
		"lambda", "update-function-code",
		"--function-name", "checkout",
		"--zip-file", "fileb://build/function.zip",
		"--publish",
		"--query", "Version",
		"--output", "text",
		"--region", "us-west-2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updateZipDirectArgs\n got %q\nwant %q", got, want)
	}
}

func TestS3StageArgs_CopiesZipToBucketKey(t *testing.T) {
	got := s3StageArgs("build/function.zip", "artifacts", "checkout/function.zip", "us-west-2", testProfile)
	want := []string{
		"s3", "cp",
		"build/function.zip",
		"s3://artifacts/checkout/function.zip",
		"--region", "us-west-2",
		"--profile", "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("s3StageArgs\n got %q\nwant %q", got, want)
	}
}

func TestUpdateAliasArgs_PointsAliasAtVersion(t *testing.T) {
	got := updateAliasArgs("checkout", "live", "42", "us-west-2", testProfile)
	want := []string{
		"lambda", "update-alias",
		"--function-name", "checkout",
		"--name", "live",
		"--function-version", "42",
		"--region", "us-west-2",
		"--profile", "dev",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updateAliasArgs\n got %q\nwant %q", got, want)
	}
}

func TestImageDeployConfig_Defaults(t *testing.T) {
	c := ImageDeployConfig{FunctionName: "f", ImageURI: "uri"}
	c.applyDefaults()
	if c.Alias != "live" {
		t.Errorf("Alias = %q, want live", c.Alias)
	}
	if c.Region != "" {
		t.Errorf("Region = %q, want empty (no hardcoded default)", c.Region)
	}
}

func TestZipDeployConfig_Defaults(t *testing.T) {
	c := ZipDeployConfig{FunctionName: "f"}
	c.applyDefaults()
	if c.ZipPath != "function.zip" {
		t.Errorf("ZipPath = %q, want function.zip", c.ZipPath)
	}
	if c.Alias != "live" {
		t.Errorf("Alias = %q, want live", c.Alias)
	}
	if c.Region != "" {
		t.Errorf("Region = %q, want empty (no hardcoded default)", c.Region)
	}
	if c.ArtifactKey != "" {
		t.Errorf("ArtifactKey = %q, want empty when no bucket", c.ArtifactKey)
	}
}

func TestZipDeployConfig_ArtifactKeyDefaultsToZipBaseName(t *testing.T) {
	c := ZipDeployConfig{FunctionName: "f", ZipPath: "build/dist/handler.zip", ArtifactBucket: "artifacts"}
	c.applyDefaults()
	if c.ArtifactKey != "handler.zip" {
		t.Errorf("ArtifactKey = %q, want handler.zip", c.ArtifactKey)
	}
}

func TestRollbackConfig_Defaults(t *testing.T) {
	c := RollbackConfig{FunctionName: "f", Version: "3"}
	c.applyDefaults()
	if c.Alias != "live" {
		t.Errorf("Alias = %q, want live", c.Alias)
	}
	if c.Region != "" {
		t.Errorf("Region = %q, want empty (no hardcoded default)", c.Region)
	}
}

func TestDryRunEnabled_ExplicitFlag(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "")
	if !dryRunEnabled(true) {
		t.Error("dryRunEnabled(true) = false, want true")
	}
	if dryRunEnabled(false) {
		t.Error("dryRunEnabled(false) = true, want false")
	}
}

func TestDryRunEnabled_EnvVar(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if !dryRunEnabled(false) {
		t.Error("dryRunEnabled(false) with SPARKWING_DRY_RUN set = false, want true")
	}
}

func TestDeployImage_DryRunSkipsExecutionAndReturnsEmptyPrev(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "")
	prev, err := DeployImage(context.Background(), ImageDeployConfig{
		FunctionName: "checkout",
		ImageURI:     "uri",
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("DeployImage dry-run err = %v", err)
	}
	if prev != "" {
		t.Errorf("prevVersion = %q, want empty under dry-run", prev)
	}
}

func TestDeployImage_DryRunViaEnvVar(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if _, err := DeployImage(context.Background(), ImageDeployConfig{
		FunctionName: "checkout",
		ImageURI:     "uri",
	}); err != nil {
		t.Fatalf("DeployImage env dry-run err = %v", err)
	}
}

func TestDeployImage_RequiresFunctionName(t *testing.T) {
	if _, err := DeployImage(context.Background(), ImageDeployConfig{ImageURI: "uri"}); err == nil {
		t.Error("expected error for missing FunctionName")
	}
}

func TestDeployImage_RequiresImageURI(t *testing.T) {
	if _, err := DeployImage(context.Background(), ImageDeployConfig{FunctionName: "f"}); err == nil {
		t.Error("expected error for missing ImageURI")
	}
}

func TestDeployZip_DryRunDirectUpload(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "")
	prev, err := DeployZip(context.Background(), ZipDeployConfig{
		FunctionName: "checkout",
		ZipPath:      "function.zip",
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("DeployZip direct dry-run err = %v", err)
	}
	if prev != "" {
		t.Errorf("prevVersion = %q, want empty under dry-run", prev)
	}
}

func TestDeployZip_DryRunS3Staged(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "")
	if _, err := DeployZip(context.Background(), ZipDeployConfig{
		FunctionName:   "checkout",
		ZipPath:        "function.zip",
		ArtifactBucket: "artifacts",
		DryRun:         true,
	}); err != nil {
		t.Fatalf("DeployZip staged dry-run err = %v", err)
	}
}

func TestDeployZip_RequiresFunctionName(t *testing.T) {
	if _, err := DeployZip(context.Background(), ZipDeployConfig{ZipPath: "function.zip"}); err == nil {
		t.Error("expected error for missing FunctionName")
	}
}

func TestRollback_DryRunSkipsExecution(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := Rollback(context.Background(), RollbackConfig{
		FunctionName: "checkout",
		Version:      "41",
	}); err != nil {
		t.Fatalf("Rollback dry-run err = %v", err)
	}
}

func TestRollback_DryRunViaConfigField(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "")
	if err := Rollback(context.Background(), RollbackConfig{
		FunctionName: "checkout",
		Version:      "41",
		DryRun:       true,
	}); err != nil {
		t.Fatalf("Rollback config dry-run err = %v", err)
	}
}

func TestRollback_RequiresVersion(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := Rollback(context.Background(), RollbackConfig{FunctionName: "f"}); err == nil {
		t.Error("expected error for missing Version")
	}
}

func TestRollback_RequiresFunctionName(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := Rollback(context.Background(), RollbackConfig{Version: "1"}); err == nil {
		t.Error("expected error for missing FunctionName")
	}
}
