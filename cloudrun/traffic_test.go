package cloudrun

import (
	"context"
	"reflect"
	"testing"
)

func TestTrafficArgs_ToRevisionDefaultPercent(t *testing.T) {
	clearGCPEnv(t)
	got := trafficArgs(TrafficConfig{Service: "api", Region: "us-west1", Project: "p", Revision: "api-00007-xyz"})
	want := []string{
		"run", "services", "update-traffic", "api",
		"--region", "us-west1",
		"--project", "p",
		"--to-revisions", "api-00007-xyz=100",
		"--quiet",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trafficArgs = %v\nwant %v", got, want)
	}
}

func TestTrafficArgs_ExplicitPercent(t *testing.T) {
	clearGCPEnv(t)
	got := trafficArgs(TrafficConfig{Service: "api", Region: "us-west1", Revision: "api-00007-xyz", Percent: 20})
	want := []string{
		"run", "services", "update-traffic", "api",
		"--region", "us-west1",
		"--to-revisions", "api-00007-xyz=20",
		"--quiet",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trafficArgs = %v\nwant %v", got, want)
	}
}

func TestTrafficArgs_ToLatest(t *testing.T) {
	clearGCPEnv(t)
	got := trafficArgs(TrafficConfig{Service: "api", Region: "us-west1", ToLatest: true})
	want := []string{
		"run", "services", "update-traffic", "api",
		"--region", "us-west1",
		"--to-latest",
		"--quiet",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trafficArgs = %v\nwant %v", got, want)
	}
}

func TestRemoveTagArgs(t *testing.T) {
	clearGCPEnv(t)
	got := removeTagArgs(DeployConfig{Service: "web", Region: "us-west1", Project: "p", Tag: "pr-42"})
	want := []string{
		"run", "services", "update-traffic", "web",
		"--region", "us-west1",
		"--project", "p",
		"--remove-tags", "pr-42",
		"--quiet",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("removeTagArgs = %v\nwant %v", got, want)
	}
}

func TestRevisionsListArgs(t *testing.T) {
	clearGCPEnv(t)
	got := revisionsListArgs(Ref{Service: "api", Region: "us-west1", Project: "p"})
	want := []string{
		"run", "revisions", "list", "--service", "api",
		"--region", "us-west1",
		"--project", "p",
		"--format=json", "--sort-by=~metadata.creationTimestamp",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("revisionsListArgs = %v\nwant %v", got, want)
	}
}

func TestRevisionTraffic_ReusesCoordinates(t *testing.T) {
	got := revisionTraffic(RollbackConfig{Service: "api", Region: "us-west1", Project: "p"}, "api-00006-old")
	want := TrafficConfig{Service: "api", Region: "us-west1", Project: "p", Revision: "api-00006-old"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("revisionTraffic = %+v\nwant %+v", got, want)
	}
}

const revisionsJSON = `[
  {"metadata": {"name": "api-00008-abc"}},
  {"metadata": {"name": "api-00007-xyz"}},
  {"metadata": {"name": "api-00006-old"}}
]`

func TestParseRevisionNames_Order(t *testing.T) {
	got := parseRevisionNames([]byte(revisionsJSON))
	want := []string{"api-00008-abc", "api-00007-xyz", "api-00006-old"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRevisionNames = %v\nwant %v", got, want)
	}
}

func TestParseRevisionNames_Garbage(t *testing.T) {
	if got := parseRevisionNames([]byte("nope")); got != nil {
		t.Fatalf("parseRevisionNames(garbage) = %v, want nil", got)
	}
}

const revisionsReadyJSON = `[
  {"metadata": {"name": "api-00008-new"}, "status": {"conditions": [{"type": "Ready", "status": "False"}]}},
  {"metadata": {"name": "api-00007-broken"}, "status": {"conditions": [{"type": "Ready", "status": "False"}]}},
  {"metadata": {"name": "api-00006-good"}, "status": {"conditions": [{"type": "Active", "status": "True"}, {"type": "Ready", "status": "True"}]}}
]`

func TestParseRevisions_Readiness(t *testing.T) {
	got := parseRevisions([]byte(revisionsReadyJSON))
	want := []revisionEntry{
		{name: "api-00008-new", ready: false},
		{name: "api-00007-broken", ready: false},
		{name: "api-00006-good", ready: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRevisions = %+v\nwant %+v", got, want)
	}
}

func TestPriorReadyRevisionName_SkipsLatestAndUnready(t *testing.T) {
	got := priorReadyRevisionName(parseRevisions([]byte(revisionsReadyJSON)))
	if got != "api-00006-good" {
		t.Fatalf("priorReadyRevisionName = %q, want api-00006-good", got)
	}
}

func TestPriorReadyRevisionName_NoQualifyingRevision(t *testing.T) {
	if got := priorReadyRevisionName(nil); got != "" {
		t.Fatalf("priorReadyRevisionName(nil) = %q, want empty", got)
	}
	single := []revisionEntry{{name: "api-00001-only", ready: true}}
	if got := priorReadyRevisionName(single); got != "" {
		t.Fatalf("priorReadyRevisionName(single) = %q, want empty (latest is skipped)", got)
	}
}

func TestTraffic_DryRunNoExec(t *testing.T) {
	clearGCPEnv(t)
	t.Setenv("SPARKWING_DRY_RUN", "1")
	err := Traffic(TrafficConfig{Service: "api", Region: "us-west1", Project: "p", Revision: "api-00007-xyz"})(context.Background())
	if err != nil {
		t.Fatalf("Traffic dry-run = %v, want nil", err)
	}
}

func TestRollbackToRevision_DryRunNoExecWithoutRevision(t *testing.T) {
	clearGCPEnv(t)
	err := RollbackToRevision(RollbackConfig{Service: "api", Region: "us-west1", Project: "p", DryRun: true})(context.Background())
	if err != nil {
		t.Fatalf("RollbackToRevision dry-run = %v, want nil (echoes PRIOR_REVISION, no discovery)", err)
	}
}

func TestRollback_IsRollbackToRevision(t *testing.T) {
	clearGCPEnv(t)
	err := Rollback(RollbackConfig{Service: "api", Region: "us-west1", Project: "p", Revision: "api-00006-old", DryRun: true})(context.Background())
	if err != nil {
		t.Fatalf("Rollback dry-run = %v, want nil", err)
	}
}

func TestRemoveTag_DryRunNoExec(t *testing.T) {
	clearGCPEnv(t)
	t.Setenv("SPARKWING_DRY_RUN", "1")
	err := RemoveTag(context.Background(), DeployConfig{Service: "web", Region: "us-west1", Project: "p", Tag: "pr-42"})
	if err != nil {
		t.Fatalf("RemoveTag dry-run = %v, want nil", err)
	}
}
