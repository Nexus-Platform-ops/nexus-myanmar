package moderation

import "testing"

func TestParseAction(t *testing.T) {
	for _, v := range []struct {
		action, kind, decision string
	}{
		{"MODERATE_post_REMOVED", "post", "REMOVED"},
		{"MODERATE_comment_LIVE", "comment", "LIVE"},
		{"MODERATE_event_REMOVED", "event", "REMOVED"},
		{"MODERATE_post_UNDER_REVIEW", "", ""},
		{"APPEAL_UPHELD", "", ""},
		{"", "", ""},
	} {
		kind, decision := parseAction(v.action)
		if kind != v.kind || decision != v.decision {
			t.Errorf("parseAction(%q) = %q,%q want %q,%q", v.action, kind, decision, v.kind, v.decision)
		}
	}
}

func TestValidUUID(t *testing.T) {
	if !validUUID("6f4f1d7e-63f5-4f5c-9a7a-1d3f8f0f7e11") {
		t.Fatal("valid uuid rejected")
	}
	if validUUID("not-a-uuid") || validUUID("") {
		t.Fatal("invalid uuid accepted")
	}
}
