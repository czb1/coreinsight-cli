package cli

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestLatestSceneSkillVersions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{
			name: "latest-upload-first",
			input: `[{"versions":[
				{"version":"0.2.2","uploadDate":"2026-10-12 14:25:21"},
				{"version":"0.2.1","uploadDate":"2026-10-10 17:32:24"}
			]}]`,
			want: `[{"versions":[{"version":"0.2.2","uploadDate":"2026-10-12 14:25:21"}]}]`,
		},
		{
			name: "upload-date-takes-precedence",
			input: `[{"versions":[
				{"version":"2.0.0","uploadDate":"2026-10-10 12:00:00"},
				{"version":"1.5.1","uploadDate":"2026-10-12 12:00:00"}
			]}]`,
			want: `[{"versions":[{"version":"1.5.1","uploadDate":"2026-10-12 12:00:00"}]}]`,
		},
		{
			name:  "missing-dates-use-numeric-version",
			input: `[{"versions":[{"version":"0.2.9"},{"version":"0.2.10"},{"version":"0.2.2"}]}]`,
			want:  `[{"versions":[{"version":"0.2.10"}]}]`,
		},
		{
			name: "equal-dates-use-numeric-version",
			input: `[{"versions":[
				{"version":"0.9.9","uploadDate":"2026-10-12 12:00:00"},
				{"version":"v1.0","uploadDate":"2026-10-12 12:00:00"}
			]}]`,
			want: `[{"versions":[{"version":"v1.0","uploadDate":"2026-10-12 12:00:00"}]}]`,
		},
		{
			name: "invalid-dates-use-numeric-version",
			input: `[{"versions":[
				{"version":"1.0.0","uploadDate":"not-a-date"},
				{"version":"1.0.1","uploadDate":null}
			]}]`,
			want: `[{"versions":[{"version":"1.0.1","uploadDate":null}]}]`,
		},
		{
			name: "valid-date-beats-invalid-or-missing",
			input: `[{"versions":[
				{"version":"3.0.0","uploadDate":"bad"},
				{"version":"1.0.0","uploadDate":"2026-10-12 12:00:00"},
				{"version":"2.0.0"}
			]}]`,
			want: `[{"versions":[{"version":"1.0.0","uploadDate":"2026-10-12 12:00:00"}]}]`,
		},
		{
			name: "timestamps-compare-instants",
			input: `[{"versions":[
				{"version":"1.0.0","uploadDate":"2026-10-12 14:25:21"},
				{"version":"1.0.1","uploadDate":"2026-10-12T06:25:22Z"},
				{"version":"1.0.2","uploadDate":"2026-10-12T14:25:20+08:00"}
			]}]`,
			want: `[{"versions":[{"version":"1.0.1","uploadDate":"2026-10-12T06:25:22Z"}]}]`,
		},
		{
			name:  "equal-versions-preserve-first-record",
			input: `[{"versions":[{"version":"1.0","downloadUrl":"first.zip"},{"version":"1.0.0","downloadUrl":"second.zip"}]}]`,
			want:  `[{"versions":[{"version":"1.0","downloadUrl":"first.zip"}]}]`,
		},
		{
			name:  "unknown-labels-preserve-backend-order",
			input: `[{"versions":[{"version":"release-b"},{"version":"release-a"}]}]`,
			want:  `[{"versions":[{"version":"release-b"}]}]`,
		},
		{
			name:  "null-entries-do-not-hide-version-records",
			input: `[{"versions":[null,{"version":"1.0.0"},{"version":"1.0.1"}]}]`,
			want:  `[{"versions":[{"version":"1.0.1"}]}]`,
		},
		{
			name:  "empty-missing-null-and-single-versions-unchanged",
			input: `[{"skillName":"empty","versions":[]},{"skillName":"missing"},{"versions":null},{"versions":[{"version":"1.0.0","downloadUrl":"only.zip"}]}]`,
			want:  `[{"skillName":"empty","versions":[]},{"skillName":"missing"},{"versions":null},{"versions":[{"version":"1.0.0","downloadUrl":"only.zip"}]}]`,
		},
		{name: "empty-skills", input: `[]`, want: `[]`},
		{name: "null-skills", input: `null`, want: `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input, want interface{}
			if err := json.Unmarshal([]byte(tc.input), &input); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if got := latestSceneSkillVersions(input); !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%#v want=%#v", got, want)
			}
		})
	}
}
