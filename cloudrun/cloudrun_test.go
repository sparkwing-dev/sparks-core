package cloudrun

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/sparkwing-dev/sparkwing/sparkwing"
)

func clearGCPEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GOOGLE_CLOUD_PROJECT", "")
	t.Setenv("CLOUDSDK_CORE_PROJECT", "")
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "")
	t.Setenv("SPARKWING_DRY_RUN", "")
}

func TestDeployArgs_Image(t *testing.T) {
	clearGCPEnv(t)
	got := deployArgs(DeployConfig{
		Service:              "api",
		Image:                "us-west1-docker.pkg.dev/p/r/api:abc",
		Region:               "us-west1",
		Project:              "my-proj",
		Port:                 8080,
		AllowUnauthenticated: true,
	})
	want := []string{
		"run", "deploy", "api",
		"--image", "us-west1-docker.pkg.dev/p/r/api:abc",
		"--region", "us-west1",
		"--project", "my-proj",
		"--port", "8080",
		"--allow-unauthenticated",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestDeployArgs_SourceOverridesImage(t *testing.T) {
	clearGCPEnv(t)
	got := deployArgs(DeployConfig{
		Service: "api",
		Image:   "ignored",
		Source:  "./svc",
		Region:  "us-west1",
		Project: "p",
	})
	want := []string{
		"run", "deploy", "api",
		"--source", "./svc",
		"--region", "us-west1",
		"--project", "p",
		"--no-allow-unauthenticated",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestDeployArgs_TaggedPreviewNoTraffic(t *testing.T) {
	clearGCPEnv(t)
	got := deployArgs(DeployConfig{
		Service:   "web",
		Image:     "img",
		Region:    "us-west1",
		Project:   "p",
		Port:      3000,
		NoTraffic: true,
		Tag:       "pr-42",
	})
	want := []string{
		"run", "deploy", "web",
		"--image", "img",
		"--region", "us-west1",
		"--project", "p",
		"--port", "3000",
		"--no-allow-unauthenticated",
		"--no-traffic",
		"--tag", "pr-42",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestDeployArgs_EnvIsSortedAndJoined(t *testing.T) {
	clearGCPEnv(t)
	got := deployArgs(DeployConfig{
		Service: "api",
		Image:   "img",
		Env:     map[string]string{"ZED": "1", "ALPHA": "2", "MID": "3"},
	})
	want := []string{
		"run", "deploy", "api",
		"--image", "img",
		"--set-env-vars", "ALPHA=2,MID=3,ZED=1",
		"--no-allow-unauthenticated",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestDeployArgs_Impersonation(t *testing.T) {
	clearGCPEnv(t)
	t.Setenv("CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT", "deployer@p.iam.gserviceaccount.com")
	got := deployArgs(DeployConfig{Service: "api", Image: "img", Project: "p"})
	want := []string{
		"run", "deploy", "api",
		"--image", "img",
		"--project", "p",
		"--impersonate-service-account", "deployer@p.iam.gserviceaccount.com",
		"--no-allow-unauthenticated",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestDeployArgs_NoProjectOmitsFlag(t *testing.T) {
	clearGCPEnv(t)
	got := deployArgs(DeployConfig{Service: "api", Image: "img"})
	want := []string{
		"run", "deploy", "api",
		"--image", "img",
		"--no-allow-unauthenticated",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestDeployArgs_ResourceKnobsAndServiceAccount(t *testing.T) {
	clearGCPEnv(t)
	got := deployArgs(DeployConfig{
		Service:        "api",
		Image:          "img",
		Memory:         "1Gi",
		CPU:            "2",
		MinInstances:   1,
		MaxInstances:   10,
		Concurrency:    40,
		Timeout:        "300s",
		ServiceAccount: "run@p.iam.gserviceaccount.com",
	})
	want := []string{
		"run", "deploy", "api",
		"--image", "img",
		"--memory", "1Gi",
		"--cpu", "2",
		"--min-instances", "1",
		"--max-instances", "10",
		"--concurrency", "40",
		"--timeout", "300s",
		"--service-account", "run@p.iam.gserviceaccount.com",
		"--no-allow-unauthenticated",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestDeployArgs_ExtraArgsBeforeQuiet(t *testing.T) {
	clearGCPEnv(t)
	got := deployArgs(DeployConfig{
		Service:   "api",
		Image:     "img",
		ExtraArgs: []string{"--vpc-connector", "conn", "--ingress", "internal"},
	})
	want := []string{
		"run", "deploy", "api",
		"--image", "img",
		"--no-allow-unauthenticated",
		"--vpc-connector", "conn",
		"--ingress", "internal",
		"--quiet", "--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployArgs = %v\nwant %v", got, want)
	}
}

func TestJoinEnv_CommaValueUsesDelimiterEscape(t *testing.T) {
	got := joinEnv(map[string]string{"ALLOW": "a,b,c", "NAME": "svc"})
	if got != "^@^ALLOW=a,b,c@NAME=svc" {
		t.Fatalf("joinEnv = %q, want ^@^ALLOW=a,b,c@NAME=svc", got)
	}
}

func TestJoinEnv_DelimiterAvoidsPresentChars(t *testing.T) {
	got := joinEnv(map[string]string{"EMAIL": "a@b.com", "LIST": "x,y"})
	if got != "^#^EMAIL=a@b.com#LIST=x,y" {
		t.Fatalf("joinEnv = %q, want ^#^EMAIL=a@b.com#LIST=x,y", got)
	}
}

func TestIsNotFound(t *testing.T) {
	notFound := &sparkwing.ExecError{Stderr: "ERROR: (gcloud.run.services.describe) NOT_FOUND: Resource ..."}
	if !isNotFound(notFound) {
		t.Fatal("isNotFound(NOT_FOUND stderr) = false, want true")
	}
	if isNotFound(errors.New("network unreachable")) {
		t.Fatal("isNotFound(plain network error) = true, want false")
	}
	transient := &sparkwing.ExecError{Stderr: "ERROR: (gcloud) Your credentials could not be refreshed"}
	if isNotFound(transient) {
		t.Fatal("isNotFound(auth error) = true, want false")
	}
}

func TestDescribeArgs(t *testing.T) {
	clearGCPEnv(t)
	got := describeArgs(Ref{Service: "api", Region: "us-west1", Project: "p"})
	want := []string{
		"run", "services", "describe", "api",
		"--region", "us-west1",
		"--project", "p",
		"--format=json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("describeArgs = %v\nwant %v", got, want)
	}
}

func TestJoinEnv_Deterministic(t *testing.T) {
	got := joinEnv(map[string]string{"B": "2", "A": "1", "C": "3"})
	if got != "A=1,B=2,C=3" {
		t.Fatalf("joinEnv = %q, want A=1,B=2,C=3", got)
	}
}

func TestJoinEnv_Empty(t *testing.T) {
	if got := joinEnv(nil); got != "" {
		t.Fatalf("joinEnv(nil) = %q, want empty", got)
	}
}

const describeJSON = `{
  "status": {
    "url": "https://api-abc-uw.a.run.app",
    "latestReadyRevisionName": "api-00007-xyz",
    "latestCreatedRevisionName": "api-00008-abc",
    "traffic": [
      {"revisionName": "api-00007-xyz", "percent": 100, "url": ""},
      {"revisionName": "api-00008-abc", "tag": "pr-42", "url": "https://pr-42---api-abc-uw.a.run.app"}
    ]
  }
}`

func TestParseServiceURL(t *testing.T) {
	if got := parseServiceURL([]byte(describeJSON)); got != "https://api-abc-uw.a.run.app" {
		t.Fatalf("parseServiceURL = %q", got)
	}
}

func TestParseServiceURL_Garbage(t *testing.T) {
	if got := parseServiceURL([]byte("not json")); got != "" {
		t.Fatalf("parseServiceURL(garbage) = %q, want empty", got)
	}
}

func TestParseTaggedURL_Match(t *testing.T) {
	if got := parseTaggedURL([]byte(describeJSON), "pr-42"); got != "https://pr-42---api-abc-uw.a.run.app" {
		t.Fatalf("parseTaggedURL = %q", got)
	}
}

func TestParseTaggedURL_NoMatch(t *testing.T) {
	if got := parseTaggedURL([]byte(describeJSON), "absent"); got != "" {
		t.Fatalf("parseTaggedURL(absent) = %q, want empty", got)
	}
}

func TestParseLatestReadyRevision(t *testing.T) {
	if got := parseLatestReadyRevision([]byte(describeJSON)); got != "api-00007-xyz" {
		t.Fatalf("parseLatestReadyRevision = %q", got)
	}
}

func TestParseLatestCreatedRevision(t *testing.T) {
	if got := parseLatestCreatedRevision([]byte(describeJSON)); got != "api-00008-abc" {
		t.Fatalf("parseLatestCreatedRevision = %q", got)
	}
}

func TestIsDryRun(t *testing.T) {
	t.Setenv("SPARKWING_DRY_RUN", "")
	if isDryRun(false) {
		t.Fatal("isDryRun(false) with env unset = true, want false")
	}
	if !isDryRun(true) {
		t.Fatal("isDryRun(true) = false, want true (per-call override)")
	}
	t.Setenv("SPARKWING_DRY_RUN", "1")
	if !isDryRun(false) {
		t.Fatal("isDryRun(false) with env set = false, want true")
	}
}

func TestDeploy_DryRunEchoesNoExec(t *testing.T) {
	clearGCPEnv(t)
	t.Setenv("SPARKWING_DRY_RUN", "1")
	res, err := Deploy(context.Background(), DeployConfig{Service: "api", Image: "img", Region: "us-west1", Project: "p"})
	if err != nil {
		t.Fatalf("Deploy dry-run = %v, want nil", err)
	}
	if res == nil || res.URL != "" || res.Revision != "" || res.PriorRevision != "" {
		t.Fatalf("Deploy dry-run result = %+v, want empty non-nil", res)
	}
}

func TestDeploySource_DryRunDefaultsSource(t *testing.T) {
	clearGCPEnv(t)
	res, err := DeploySource(context.Background(), DeployConfig{Service: "api", Region: "us-west1", Project: "p", DryRun: true})
	if err != nil {
		t.Fatalf("DeploySource dry-run = %v, want nil", err)
	}
	if res == nil {
		t.Fatal("DeploySource dry-run result = nil, want non-nil")
	}
}
