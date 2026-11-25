package main

import "testing"

func TestManualCoverOrder(t *testing.T) {
	a, e := newApp(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	a.state.Items = []*Item{{ID: "post", State: "new", Images: []string{"screen.jpg", "art.jpg", "ui.jpg"}, MediaSources: []string{"screen-src", "art-src", "ui-src"}, Verified: true}}
	if e = a.moveImage("post", 1, 0); e != nil {
		t.Fatal(e)
	}
	it := a.state.Items[0]
	if it.Images[0] != "art.jpg" || it.MediaSources[0] != "art-src" || it.Verified {
		t.Fatalf("bad order %#v", it)
	}
	if e = a.moveImage("post", -1, 1); e == nil {
		t.Fatal("bad index")
	}
	it.State = "approved"
	if e = a.moveImage("post", 0, 1); e == nil {
		t.Fatal("approved should not reorder")
	}
}
