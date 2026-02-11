package rtc

import (
	"testing"
	"time"
)

func TestIssueParseAndConsumeTicket(t *testing.T) {
	svc := NewTokenService("unit-test-secret", 5*time.Second)
	ticket, claims, err := svc.Issue(IssueTicketInput{
		ServerID:  "srv_local",
		ChannelID: "vc_general",
		UserUID:   "uid_a",
		DeviceID:  "dev_a",
		Permissions: Permissions{
			Speak:       true,
			Video:       true,
			Screenshare: false,
		},
	})
	if err != nil {
		t.Fatalf("issue ticket failed: %v", err)
	}

	parsed, err := svc.ParseAndConsume(ticket)
	if err != nil {
		t.Fatalf("parse ticket failed: %v", err)
	}
	if parsed.ChannelID != claims.ChannelID || parsed.UserUID != claims.UserUID {
		t.Fatalf("parsed claims mismatch")
	}

	_, err = svc.ParseAndConsume(ticket)
	if err != ErrReplayTicket {
		t.Fatalf("expected replay error, got: %v", err)
	}
}
