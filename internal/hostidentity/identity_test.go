package hostidentity

import (
	"context"
	"strings"
	"testing"
)

func TestNativeIdentityIsStableAndHashed(t *testing.T) {
	first, err := ID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := ID(context.Background())
	if err != nil || first != second || !Valid(first) {
		t.Fatal("host identity changed or is invalid", err)
	}
	raw, err := nativeID(context.Background())
	if err != nil || first == strings.TrimSpace(raw) {
		t.Fatal("raw host identifier was exposed", err)
	}
	for _, invalid := range []string{"", "device-id", strings.Repeat("g", 64), strings.Repeat("A", 64)} {
		if Valid(invalid) {
			t.Fatal("accepted invalid host identity")
		}
	}
}
