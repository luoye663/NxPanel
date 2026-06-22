package systemmetrics

import "testing"

func TestMarkCountedDisksMarksDuplicateMounts(t *testing.T) {
	disks := []DiskUsage{
		{Mountpoint: "/mnt/data/bind", UsageKey: "8:1"},
		{Mountpoint: "/mnt/data", UsageKey: "8:1"},
		{Mountpoint: "/", UsageKey: "8:2"},
	}

	markCountedDisks(disks)

	if disks[0].Counted {
		t.Fatalf("bind mount should not be counted")
	}
	if disks[0].DuplicateOf != "/mnt/data" {
		t.Fatalf("duplicate_of = %q, want /mnt/data", disks[0].DuplicateOf)
	}
	if !disks[1].Counted {
		t.Fatalf("shorter mountpoint should be counted")
	}
	if !disks[2].Counted {
		t.Fatalf("independent filesystem should be counted")
	}
}

func TestMarkCountedDisksPrefersRootMountpoint(t *testing.T) {
	disks := []DiskUsage{
		{Mountpoint: "/var/lib/bind", UsageKey: "8:1"},
		{Mountpoint: "/", UsageKey: "8:1"},
	}

	markCountedDisks(disks)

	if disks[0].Counted {
		t.Fatalf("duplicate bind mount should not be counted")
	}
	if !disks[1].Counted {
		t.Fatalf("root mountpoint should be counted")
	}
	if disks[0].DuplicateOf != "/" {
		t.Fatalf("duplicate_of = %q, want /", disks[0].DuplicateOf)
	}
}
