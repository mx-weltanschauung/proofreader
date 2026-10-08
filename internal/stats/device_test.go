package stats

import "testing"

func TestDeviceClassAcceptsOnlyTheThreeBuckets(t *testing.T) {
	for in, want := range map[string]string{
		"narrow": "narrow", "medium": "medium", "wide": "wide",
		"": "", "phone": "", "NARROW": "", " narrow": "", "375": "",
		"narrow-but-very-long-garbage": "",
	} {
		if got := DeviceClass(in); got != want {
			t.Errorf("DeviceClass(%q) = %q, ждали %q", in, got, want)
		}
	}
}
