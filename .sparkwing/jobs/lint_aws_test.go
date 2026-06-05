package jobs

import (
	"reflect"
	"testing"
)

func TestScanGoForRawAWS(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []int
	}{
		{
			name: "aws with ProfileArgs in same func is clean",
			src: `package p
func deploy(ctx any) error {
	args := []string{"s3", "sync", "x", "y"}
	args = append(args, aws.ProfileArgs(prof)...)
	return Exec(ctx, "aws", args...)
}`,
			want: nil,
		},
		{
			name: "aws with no profile resolution is flagged",
			src: `package p
func deploy(ctx any) error {
	return Exec(ctx, "aws", "s3", "sync", "x", "y")
}`,
			want: []int{3},
		},
		{
			name: "aws in a closure sees ProfileArgs in enclosing func",
			src: `package p
func deploy(ctx any) error {
	args := append(base, aws.ProfileArgs(prof)...)
	return Run(ctx, "inv", func(ctx any) error {
		return Exec(ctx, "aws", args...)
	})
}`,
			want: nil,
		},
		{
			name: "bash aws line without profile is flagged",
			src: `package p
func login(ctx any) error {
	return Bash(ctx, "aws ecr get-login-password | docker login")
}`,
			want: []int{3},
		},
		{
			name: "ProfileFlag also counts as resolution",
			src: `package p
func login(ctx any) error {
	pf := aws.ProfileFlag(prof)
	return Bash(ctx, "aws ecr get-login-password"+pf)
}`,
			want: nil,
		},
		{
			name: "function without aws is ignored",
			src: `package p
func other(ctx any) error { return Exec(ctx, "kubectl", "get", "pods") }`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := scanGoForRawAWS("x.go", []byte(tc.src))
			if err != nil {
				t.Fatalf("scanGoForRawAWS: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("scanGoForRawAWS = %v, want %v", got, tc.want)
			}
		})
	}
}
