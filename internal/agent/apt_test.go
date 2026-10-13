package agent

import (
	"strings"
	"testing"
)

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

func TestIsDpkgLockError(t *testing.T) {
	for out, want := range map[string]bool{
		"dpkg: error: dpkg frontend lock was locked by another process with pid 5275\nNote: removing the lock file is always wrong":   true,
		"dpkg: error: dpkg status database is locked by another process":                                                              true,
		"dpkg: error processing package ros-humble-foo (--configure):\n installed post-installation script subprocess returned error": false,
	} {
		if got := isDpkgLockError([]byte(out)); got != want {
			t.Errorf("isDpkgLockError(%q) = %v", out[:40], got)
		}
	}
}

func TestPackageNamesWithDistroAndGlobs(t *testing.T) {
	if err := ValidatePackageNames(Nav2LaptopPackages); err != nil {
		t.Fatal(err)
	}
	got := ExpandROSDistro([]string{"ros-$ROS_DISTRO-navigation2", "ros-$ROS_DISTRO-nav2-minimal-tb*", "ros-${ROS_DISTRO}-rviz2"}, "humble")
	want := []string{"ros-humble-navigation2", "ros-humble-nav2-minimal-tb*", "ros-humble-rviz2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
	for _, bad := range []string{"-y", "*-o", "*", "ros*", "a*b*c", "foo;rm", "$HOME", "ros-$(id)-x", "\\*"} {
		if ValidatePackageNames([]string{bad}) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestExpandPackageGlobs(t *testing.T) {
	available := map[string][]string{
		"ros-humble-nav2-minimal-tb": nil, // Humble has none
		"ros-jazzy-nav2-minimal-tb":  {"ros-jazzy-nav2-minimal-tb3-sim", "ros-jazzy-nav2-minimal-tb4-sim", "ros-jazzy-nav2-minimal-tb4-description"},
	}
	names := func(prefix string) []string { return available[prefix] }

	got := expandPackageGlobs([]string{"ros-humble-navigation2", "ros-humble-nav2-minimal-tb*", "ros-humble-turtlebot3"}, names)
	if strings.Join(got, " ") != "ros-humble-navigation2 ros-humble-turtlebot3" {
		t.Errorf("humble: %v", got)
	}
	got = expandPackageGlobs([]string{"ros-jazzy-nav2-minimal-tb*"}, names)
	if len(got) != 3 {
		t.Errorf("jazzy: %v", got)
	}
}
