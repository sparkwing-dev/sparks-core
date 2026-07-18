package docker

import (
	"context"
	"reflect"
	"testing"

	sparkwingDocker "github.com/sparkwing-dev/sparkwing/sparkwing/docker"
)

func TestBuildArgFlags_SortedDeterministic(t *testing.T) {
	got := buildArgFlags(map[string]string{"ZED": "1", "ALPHA": "2", "MID": "3"})
	want := []string{"--build-arg", "ALPHA=2", "--build-arg", "MID=3", "--build-arg", "ZED=1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildArgFlags = %v, want %v", got, want)
	}
}

func TestBuildArgFlags_EmptyIsNil(t *testing.T) {
	if got := buildArgFlags(nil); got != nil {
		t.Fatalf("buildArgFlags(nil) = %v, want nil", got)
	}
	if got := buildArgFlags(map[string]string{}); got != nil {
		t.Fatalf("buildArgFlags(empty) = %v, want nil", got)
	}
}

func TestImageRefs(t *testing.T) {
	got := imageRefs("ghcr.io/org", "myapp", []string{"v1", "latest"})
	want := []string{"ghcr.io/org/myapp:v1", "ghcr.io/org/myapp:latest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("imageRefs = %v, want %v", got, want)
	}
}

func TestBuildxArgs_FullConfig(t *testing.T) {
	cfg := BuildxConfig{
		Platforms:  "linux/amd64,linux/arm64",
		Dockerfile: "Dockerfile",
		Context:    ".",
		BuildArgs:  map[string]string{"VERSION": "1.2.3"},
		CacheFrom:  []string{"type=registry,ref=reg/app:buildcache"},
		CacheTo:    []string{"type=registry,ref=reg/app:buildcache,mode=max"},
	}
	got := buildxArgs(cfg, []string{"reg/app:v1"})
	want := []string{
		"buildx", "build",
		"--platform", "linux/amd64,linux/arm64",
		"-f", "Dockerfile",
		"-t", "reg/app:v1",
		"--build-arg", "VERSION=1.2.3",
		"--cache-from", "type=registry,ref=reg/app:buildcache",
		"--cache-to", "type=registry,ref=reg/app:buildcache,mode=max",
		"--push", ".",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildxArgs = %v, want %v", got, want)
	}
}

func TestBuildxArgs_BuilderTargetExtraArgs(t *testing.T) {
	cfg := BuildxConfig{
		Platforms:  "linux/amd64",
		Builder:    "ci-builder",
		Dockerfile: "Dockerfile",
		Target:     "runtime",
		Context:    ".",
		ExtraArgs:  []string{"--provenance=false", "--no-cache"},
	}
	got := buildxArgs(cfg, []string{"reg/app:v1"})
	want := []string{
		"buildx", "build",
		"--builder", "ci-builder",
		"--platform", "linux/amd64",
		"-f", "Dockerfile",
		"--target", "runtime",
		"-t", "reg/app:v1",
		"--provenance=false", "--no-cache",
		"--push", ".",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildxArgs = %v, want %v", got, want)
	}
}

func TestBuildxArgs_Minimal(t *testing.T) {
	got := buildxArgs(BuildxConfig{Platforms: "linux/amd64", Dockerfile: "Dockerfile", Context: "."}, []string{"reg/app:latest"})
	want := []string{
		"buildx", "build",
		"--platform", "linux/amd64",
		"-f", "Dockerfile",
		"-t", "reg/app:latest",
		"--push", ".",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildxArgs = %v, want %v", got, want)
	}
}

func TestBuildCacheRef_Local(t *testing.T) {
	from, to, err := BuildCacheRef("local", "/tmp/cache")
	if err != nil {
		t.Fatalf("BuildCacheRef local = %v", err)
	}
	if from != "type=local,src=/tmp/cache" {
		t.Errorf("cacheFrom = %q", from)
	}
	if to != "type=local,dest=/tmp/cache,mode=max" {
		t.Errorf("cacheTo = %q", to)
	}
}

func TestBuildCacheRef_LocalDefaultsDir(t *testing.T) {
	from, to, err := BuildCacheRef("", "")
	if err != nil {
		t.Fatalf("BuildCacheRef empty = %v", err)
	}
	if from != "type=local,src="+defaultLocalCacheDir {
		t.Errorf("cacheFrom = %q, want default dir", from)
	}
	if to != "type=local,dest="+defaultLocalCacheDir+",mode=max" {
		t.Errorf("cacheTo = %q, want default dir", to)
	}
}

func TestBuildCacheRef_Registry(t *testing.T) {
	for _, backend := range []string{"registry", "ecr", "gar", "ghcr"} {
		from, to, err := BuildCacheRef(backend, "reg/app:buildcache")
		if err != nil {
			t.Fatalf("BuildCacheRef %s = %v", backend, err)
		}
		if from != "type=registry,ref=reg/app:buildcache" {
			t.Errorf("%s cacheFrom = %q", backend, from)
		}
		if to != "type=registry,ref=reg/app:buildcache,mode=max" {
			t.Errorf("%s cacheTo = %q", backend, to)
		}
	}
}

func TestBuildCacheRef_RegistryRequiresRef(t *testing.T) {
	if _, _, err := BuildCacheRef("ecr", ""); err == nil {
		t.Fatal("BuildCacheRef ecr with empty ref = nil, want error")
	}
}

func TestBuildCacheRef_UnknownBackend(t *testing.T) {
	if _, _, err := BuildCacheRef("azure", "x"); err == nil {
		t.Fatal("BuildCacheRef unknown backend = nil, want error")
	}
}

func TestRegistryHost(t *testing.T) {
	cases := map[string]string{
		"ghcr.io/org":                                  "ghcr.io",
		"us-west1-docker.pkg.dev/proj/repo":            "us-west1-docker.pkg.dev",
		"633280902600.dkr.ecr.us-west-2.amazonaws.com": "633280902600.dkr.ecr.us-west-2.amazonaws.com",
	}
	for in, want := range cases {
		if got := registryHost(in); got != want {
			t.Errorf("registryHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGarLoginArgs(t *testing.T) {
	got := garLoginArgs("us-west1-docker.pkg.dev")
	want := []string{"auth", "configure-docker", "us-west1-docker.pkg.dev", "--quiet"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("garLoginArgs = %v, want %v", got, want)
	}
}

func TestGhcrLoginArgs_TokenNotInArgv(t *testing.T) {
	got := ghcrLoginArgs("ghcr.io", "octocat")
	want := []string{"login", "ghcr.io", "--username", "octocat", "--password-stdin"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ghcrLoginArgs = %v, want %v", got, want)
	}
}

func TestRegistryLogin_UnknownKind(t *testing.T) {
	if err := RegistryLogin(context.Background(), LoginConfig{Kind: "docker-hub", Registry: "x"}); err == nil {
		t.Fatal("RegistryLogin unknown kind = nil, want error")
	}
}

func TestRegistryLogin_DryRunNoExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	cases := []LoginConfig{
		{Kind: RegistryECR, Registry: "633280902600.dkr.ecr.us-west-2.amazonaws.com"},
		{Kind: "", Registry: "633280902600.dkr.ecr.us-west-2.amazonaws.com"},
		{Kind: RegistryGAR, Registry: "us-west1-docker.pkg.dev/proj/repo"},
		{Kind: RegistryGHCR, Registry: "ghcr.io/org"},
	}
	for _, cfg := range cases {
		if err := RegistryLogin(context.Background(), cfg); err != nil {
			t.Fatalf("RegistryLogin(%+v) dry-run = %v, want nil (echo, no exec)", cfg, err)
		}
	}
}

func TestECRLogin_DryRunNoExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if err := ECRLogin(context.Background(), "633280902600.dkr.ecr.us-west-2.amazonaws.com", ""); err != nil {
		t.Fatalf("ECRLogin dry-run = %v, want nil", err)
	}
}

func TestBuildxPublish_DryRunNoExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	err := BuildxPublish(context.Background(), BuildxConfig{
		Image:    "myapp",
		Registry: "ghcr.io/org",
		Tags:     []string{"v1"},
	})
	if err != nil {
		t.Fatalf("BuildxPublish dry-run = %v, want nil (echo, no exec)", err)
	}
}

func TestBuildxPublish_RequiresImageRegistryAndTags(t *testing.T) {
	if err := BuildxPublish(context.Background(), BuildxConfig{Registry: "ghcr.io/org", Tags: []string{"v1"}}); err == nil {
		t.Fatal("BuildxPublish without Image = nil, want error")
	}
	if err := BuildxPublish(context.Background(), BuildxConfig{Image: "myapp", Tags: []string{"v1"}}); err == nil {
		t.Fatal("BuildxPublish without Registry = nil, want error")
	}
	if err := BuildxPublish(context.Background(), BuildxConfig{Image: "myapp", Registry: "ghcr.io/org"}); err == nil {
		t.Fatal("BuildxPublish without Tags = nil, want error")
	}
}

func TestBuildAndPush_DryRunNoExec(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "1")
	err := BuildAndPush(context.Background(), BuildConfig{
		Image:      "myapp",
		Dockerfile: "Dockerfile",
		Registries: []string{"ghcr.io/org"},
		Tags:       sparkwingDocker.ImageTag{Commit: "abc123", Content: "deadbeef"},
	})
	if err != nil {
		t.Fatalf("BuildAndPush dry-run = %v, want nil (echo, no exec)", err)
	}
}
