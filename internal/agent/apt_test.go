package agent

import "testing"

func TestAptHelpers(t *testing.T) {
	if got := firstNonOption([]string{"-o", "DPkg::Lock::Timeout=300", "-o", "x=y", "upgrade", "-y"}); got != "upgrade" {
		t.Fatalf("firstNonOption = %q", got)
	}
	if got := firstNonOption([]string{"--configure", "-a"}); got != "--configure -a" {
		t.Fatalf("firstNonOption = %q", got)
	}
	out := []byte("Reading package lists...\n312 upgraded, 4 newly installed, 0 to remove and 2 not upgraded.\nNeed to get 400 MB\n")
	if got := string(aptSummaryPattern.Find(out)); got != "312 upgraded, 4 newly installed, 0 to remove and 2 not upgraded." {
		t.Fatalf("summary = %q", got)
	}
}

func TestParseInstalledPackagesKeepsOnlyInstalled(t *testing.T) {
	out := "ii \tros-humble-rclcpp\t16.0.9-1jammy.20240607.103020\nrc \tros-humble-old\t1.0-1\nii \tros-humble-tf2\t0.25.7-1jammy.20240607.000000\n"
	got := parseInstalledPackages(out)
	if len(got) != 2 || got["ros-humble-rclcpp"] != "16.0.9-1jammy.20240607.103020" || got["ros-humble-old"] != "" {
		t.Fatalf("got %v", got)
	}
}
