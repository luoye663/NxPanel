package accesspolicy

import (
	"strings"
	"testing"
)

func samplePolicy() Policy {
	return Policy{Mode: "unified", DefaultAction: Action{Type: "deny", StatusCode: 403}, Rules: []Rule{
		{ID: "office", Name: "Office", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "ip", Values: []string{"10.0.0.0/8", "2001:db8::/32"}}}, Action: Action{Type: "allow"}},
		{ID: "country", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "country", Values: []string{"CN"}}}, Action: Action{Type: "deny", StatusCode: 451, ResponseType: "html", ResponseBody: "<b>$uri</b>"}},
		{ID: "password", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "path", Operator: "prefix", Values: []string{"/admin"}}}, Action: Action{Type: "auth", AccountIDs: []string{"user"}}},
	}}
}
func TestFirstMatchOrderAndAuthentication(t *testing.T) {
	p := samplePolicy()
	ctx := RenderContext{CountryNetworks: map[string][]string{"CN": {"10.0.0.0/8"}}}
	got, err := Preview(p, PreviewRequest{IP: "10.2.3.4", Path: "/admin"}, ctx)
	if err != nil || got.Action.Type != "allow" {
		t.Fatalf("first allow: %#v %v", got, err)
	}
	p.Rules[0], p.Rules[1] = p.Rules[1], p.Rules[0]
	got, err = Preview(p, PreviewRequest{IP: "10.2.3.4", Path: "/admin"}, ctx)
	if err != nil || got.Action.StatusCode != 451 {
		t.Fatalf("reordered deny: %#v %v", got, err)
	}
	got, err = Preview(p, PreviewRequest{IP: "8.8.8.8", Path: "/x/../%61dmin?x=1"}, ctx)
	if err != nil || !got.RequiresAuthentication || got.MatchedRuleID != "password" {
		t.Fatalf("auth: %#v %v", got, err)
	}
	if got.Rules[2].Evaluated != true {
		t.Fatal("auth rule not marked evaluated")
	}
}
func TestConditionSemantics(t *testing.T) {
	cases := []struct {
		name      string
		condition Condition
		req       PreviewRequest
		want      bool
	}{
		{"ipv6", Condition{Kind: "ip", Values: []string{"2001:db8::/32"}}, PreviewRequest{IP: "2001:db8::1", Path: "/"}, true},
		{"negated", Condition{Kind: "ip", Values: []string{"1.1.1.1"}, Negate: true}, PreviewRequest{IP: "8.8.8.8", Path: "/"}, true},
		{"unknown", Condition{Kind: "country", Values: []string{"ZZ"}}, PreviewRequest{IP: "8.8.8.8", Path: "/"}, true},
		{"extension", Condition{Kind: "extension", Values: []string{".jpg"}}, PreviewRequest{IP: "8.8.8.8", Path: "/a.JPG?download=true"}, true},
		{"prefix is literal", Condition{Kind: "path", Operator: "prefix", Values: []string{"/a.b"}}, PreviewRequest{IP: "8.8.8.8", Path: "/axb"}, false},
		{"referer domain", Condition{Kind: "referer", Values: []string{"example.com"}}, PreviewRequest{IP: "8.8.8.8", Path: "/", Referer: "HTTPS://EXAMPLE.COM:443/path"}, true},
		{"referer spoof", Condition{Kind: "referer", Values: []string{"example.com"}}, PreviewRequest{IP: "8.8.8.8", Path: "/", Referer: "https://example.com.evil/path"}, false},
		{"referer wildcard", Condition{Kind: "referer", Values: []string{"*.example.com"}}, PreviewRequest{IP: "8.8.8.8", Path: "/", Referer: "https://a.b.example.com/path"}, true},
		{"referer none", Condition{Kind: "referer", Values: []string{"none"}}, PreviewRequest{IP: "8.8.8.8", Path: "/"}, true},
		{"blocked excludes empty", Condition{Kind: "referer", Values: []string{"blocked"}}, PreviewRequest{IP: "8.8.8.8", Path: "/"}, false},
		{"blocked", Condition{Kind: "referer", Values: []string{"blocked"}}, PreviewRequest{IP: "8.8.8.8", Path: "/", Referer: "about:blank"}, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			p := Policy{DefaultAction: Action{Type: "deny"}, Rules: []Rule{{ID: "r", Enabled: true, Match: "all", Conditions: []Condition{tt.condition}, Action: Action{Type: "allow"}}}}
			got, err := Preview(p, tt.req, RenderContext{})
			if err != nil {
				t.Fatal(err)
			}
			if (got.Action.Type == "allow") != tt.want {
				t.Fatalf("got %#v", got)
			}
		})
	}
}
func TestCompileBoundsAndInjection(t *testing.T) {
	p := samplePolicy()
	ctx := RenderContext{AuthFiles: map[string]string{"password": "user:{SHA}abc\n"}}
	compiled, err := Compile("site", "/panel", p, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.ServerContent, "_dollar}") {
		t.Fatal("response dollar not escaped through literal variable")
	}
	if strings.Contains(compiled.ServerContent, "error_page") {
		t.Fatal("must not intercept content response errors")
	}
	if !strings.Contains(compiled.GlobalContent, "volatile;") {
		t.Fatal("internal redirect guard must update")
	}
	p.Rules[0].Conditions[0].Values = []string{"127.0.0.1; return 200"}
	if err := Validate(p); err == nil {
		t.Fatal("accepted injected IP")
	}
	p = samplePolicy()
	p.Rules[0].ID = "bad\n}"
	if err := Validate(p); err == nil {
		t.Fatal("accepted injected ID")
	}
	p = samplePolicy()
	p.Rules[0].Enabled = false
	p.Rules[0].Conditions = []Condition{{Kind: "path", Operator: "prefix", Values: []string{"~ ^/admin"}}}
	if err := Validate(p); err == nil {
		t.Fatal("accepted unsupported disabled PCRE")
	}
	p = samplePolicy()
	p.Rules = make([]Rule, MaxRules+1)
	if err := Validate(p); err == nil {
		t.Fatal("accepted excessive rules")
	}
}

func TestResponseBodyByteBoundary(t *testing.T) {
	p := Policy{DefaultAction: Action{Type: "deny", ResponseBody: strings.Repeat("界", 21845) + "x"}}
	if err := Validate(p); err != nil {
		t.Fatalf("64 KiB body rejected: %v", err)
	}
	p.DefaultAction.ResponseBody += "x"
	if err := Validate(p); err == nil {
		t.Fatal("accepted body over 64 KiB")
	}
}

func TestDisabledSourceDoesNotParticipate(t *testing.T) {
	p := samplePolicy()
	p.Rules[0].SourceDisabled = true
	got, err := Preview(p, PreviewRequest{IP: "10.1.2.3", Path: "/"}, RenderContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Action.Type != "deny" || got.Rules[0].Evaluated || got.Rules[0].Matched {
		t.Fatalf("disabled source participates: %#v", got)
	}
	compiled, err := Compile("site", "/panel", p, RenderContext{AuthFiles: map[string]string{"password": "user:{SHA}abc\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(compiled.GlobalContent, "10.0.0.0/8") {
		t.Fatal("disabled source emitted IP matcher")
	}
}
func TestPreviewNormalizesTrailingDotSegments(t *testing.T) {
	p := Policy{DefaultAction: Action{Type: "deny"}, Rules: []Rule{{ID: "r", Enabled: true, Match: "all", Conditions: []Condition{{Kind: "path", Operator: "exact", Values: []string{"/admin/"}}}, Action: Action{Type: "allow"}}}}
	for _, uri := range []string{"/admin/.", "/admin/x/..", "//admin//"} {
		got, err := Preview(p, PreviewRequest{IP: "127.0.0.1", Path: uri}, RenderContext{})
		if err != nil || got.Action.Type != "allow" {
			t.Fatalf("%s: %#v %v", uri, got, err)
		}
	}
	if _, err := Preview(p, PreviewRequest{IP: "127.0.0.1", Path: "/../admin"}, RenderContext{}); err == nil {
		t.Fatal("accepted above-root traversal rejected by nginx")
	}
}
