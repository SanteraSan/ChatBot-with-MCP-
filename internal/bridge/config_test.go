package bridge

import "testing"

func TestCommandName(t *testing.T) {
	cases := map[string]string{
		"/start-fcb":          "start_fcb",
		"start-fcb":           "start_fcb",
		"/start-fcb@some_bot": "start_fcb",
		"  /commands  ":       "commands",
	}
	for in, want := range cases {
		if got := CommandName(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestWebhookURLComplete(t *testing.T) {
	if WebhookURLComplete("https://api2.cursor.sh/automations/webhook") {
		t.Fatal("prefix without id must be incomplete")
	}
	if !WebhookURLComplete("https://api2.cursor.sh/automations/webhook/abc") {
		t.Fatal("url with id must be complete")
	}
}
