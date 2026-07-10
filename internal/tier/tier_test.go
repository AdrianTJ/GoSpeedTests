package tier

import "testing"

func TestValid(t *testing.T) {
	for _, name := range Supported {
		if !Valid(name) {
			t.Errorf("Supported tier %q reported invalid", name)
		}
	}
	for _, bad := range []string{"", "bogus", "Network", "net", "ALL"} {
		if Valid(bad) {
			t.Errorf("expected %q to be invalid", bad)
		}
	}
}
