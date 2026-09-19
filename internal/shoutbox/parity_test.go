package shoutbox

import (
	"testing"
	"time"
)

// TestPythonParity documents the byte-for-byte parity with psogen.py and
// messages.py output captured from the original implementation.
func TestPythonParity(t *testing.T) {
	at := time.Unix(1758283200, 0)
	got := PrepareLine("Ana", "hello <world>", "blue", at)
	want := "<div class='message'><date>" + at.Format("15:04:05") +
		"</date> <name>Ana:</name> <data class='blue'>hello &lt;world&gt;</data></div>\n"
	if got != want {
		t.Errorf("PrepareLine parity:\n got %q\nwant %q", got, want)
	}
	msg := NewShoutboxMessage("box")
	msg.Set("<div>x</div>")
	if msg.Wire() != "piratebox;sb;01;box;PGRpdj54PC9kaXY+" {
		t.Errorf("wire parity: %q", msg.Wire())
	}
}
