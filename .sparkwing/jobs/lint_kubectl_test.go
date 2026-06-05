package jobs

import (
	"reflect"
	"testing"
)

func TestScanGoForRawKubectl(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []int
	}{
		{
			name: "exec arg-vector kubectl is flagged",
			src: `package p
func f(ctx any) { step.Exec(ctx, "kubectl", "apply", "-f", "x") }`,
			want: []int{2},
		},
		{
			name: "bash kubectl line is flagged",
			src: `package p
func f(ctx any) { sparkwing.Bash(ctx, "kubectl rollout undo deploy/x") }`,
			want: []int{2},
		},
		{
			name: "kubectl in a pipe inside bash is flagged",
			src: `package p
func f(ctx any) { sparkwing.Bash(ctx, "kustomize build . | kubectl apply -f -") }`,
			want: []int{2},
		},
		{
			name: "routing through the helper is clean",
			src: `package p
func f(ctx any) { kubectl(ctx, kubeCtx, "apply", "-k", dir) }`,
			want: nil,
		},
		{
			name: "kubectl in a log line is not flagged",
			src: `package p
func f(ctx any) { sparkwing.Info(ctx, "running kubectl apply now") }`,
			want: nil,
		},
		{
			name: "substring kubectltool is not flagged (word boundary)",
			src: `package p
func f(ctx any) { step.Exec(ctx, "kubectltool", "x") }`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanGoForRawKubectl("x.go", []byte(tc.src))
			if err != nil {
				t.Fatalf("scanGoForRawKubectl: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scanGoForRawKubectl = %v, want %v", got, tc.want)
			}
		})
	}
}
