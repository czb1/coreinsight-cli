package cli

import "testing"

func TestNormalizeGitURL(t *testing.T) {
	cases := map[string]string{
		"git@codehub-y.huawei.com:CSP/CSPCertSDK_C.git": "https://codehub-y.huawei.com/CSP/CSPCertSDK_C",
		"https://user:pass@example.com/a/b.git?x=1#f":   "https://example.com/a/b",
		"ssh://git@example.com:22/a/b.git":              "https://example.com/a/b",
	}
	for in, want := range cases {
		got, err := normalizeGitURL(in)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestRoutePayload(t *testing.T) {
	k, v, err := routePayload("1, 2", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if k != "kb_sns" || len(v) != 2 || v[1] != "2" {
		t.Fatalf("unexpected: %s %#v", k, v)
	}
	if _, _, err := routePayload("1", "P", "", ""); err == nil {
		t.Fatal("expected exclusive route error")
	}
	if _, _, err := routePayload("", "", "", ""); err == nil {
		t.Fatal("expected missing route error")
	}
}

func TestScalarString(t *testing.T) {
	if got := scalarString(float64(22633567)); got != "22633567" {
		t.Fatalf("got %q", got)
	}
}
