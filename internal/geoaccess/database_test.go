package geoaccess

import (
	"reflect"
	"testing"
)

func TestAggregateNetworks(t *testing.T) {
	got, err := aggregateNetworks([]string{
		"192.0.2.0/25", "192.0.2.128/25",
		"2001:db8::/33", "2001:db8:8000::/33",
		"198.51.100.0/24",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"192.0.2.0/24", "198.51.100.0/24", "2001:db8::/32"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("aggregateNetworks() = %#v, want %#v", got, want)
	}
}
