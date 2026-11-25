package main

import (
	"testing"
	"time"
)

func TestSlots(t *testing.T) {
	if _, err := validateSlots("10:00,14:00,18:00"); err != nil {
		t.Fatal(err)
	}
	if _, err := validateSlots("26:61"); err == nil {
		t.Fatal("bad slot allowed")
	}
}
func TestSchedulingAndMissed(t *testing.T) {
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a.state.Config.VKBrowserMode = false
	a.state.Config.VKPostponedOnApproval = false
	a.state.Items = []*Item{{ID: "a", State: "approved", Verified: true, Images: []string{"a", "b", "c"}}}
	when, e := a.setSchedule("a", "queue", "")
	if e != nil {
		t.Fatal(e)
	}
	if when.Before(time.Now()) {
		t.Fatal("past slot")
	}
	if _, e = a.setSchedule("a", "at", when.In(moscow).Format("2006-01-02T15:04")); e != nil {
		t.Fatal(e)
	}
	a.processSchedule(when.Add(time.Hour))
	if a.state.Items[0].State != "missed" {
		t.Fatalf("state = %s", a.state.Items[0].State)
	}
}
func TestApprovalRequired(t *testing.T) {
	a, _ := newApp(t.TempDir())
	a.state.Items = []*Item{{ID: "a", State: "new"}}
	if _, e := a.setSchedule("a", "queue", ""); e == nil {
		t.Fatal("scheduled unapproved post")
	}
}

func TestCalendarCollision(t *testing.T) {
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a.state.Config.VKBrowserMode = false
	a.state.Config.VKPostponedOnApproval = false
	pics := []string{"a", "b", "c"}
	a.state.Items = []*Item{{ID: "a", State: "approved", Verified: true, Images: pics}, {ID: "b", State: "approved", Verified: true, Images: pics}}
	date, e := a.setSchedule("a", "queue", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.setSchedule("b", "at", date.In(moscow).Format("2006-01-02T15:04")); e == nil {
		t.Fatal("calendar collision allowed")
	}
}
