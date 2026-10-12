package controller

import "testing"

func TestDiffPackagesIgnoresBuildStampsAndOneSidedPackages(t *testing.T) {
	robot := map[string]string{
		"ros-humble-rclcpp":             "16.0.9-1jammy.20240607.103020",
		"ros-humble-turtlebot3-msgs":    "2.2.3-1jammy.20240101.010101",
		"ros-humble-turtlebot3-node":    "2.1.5-1jammy.20240101.010101", // robot only
		"ros-humble-rmw-cyclonedds-cpp": "1.3.4-1jammy.20240607.000000",
	}
	laptop := map[string]string{
		"ros-humble-rclcpp":             "16.0.9-1jammy.20240610.221500",  // same release, other build
		"ros-humble-turtlebot3-msgs":    "2.2.4-1jammy.20240901.010101",   // newer release
		"ros-humble-rviz2":              "11.2.12-1jammy.20240901.010101", // laptop only
		"ros-humble-rmw-cyclonedds-cpp": "1.3.4-1jammy.20240607.000000",
	}
	diffs := diffPackages(robot, laptop)
	if len(diffs) != 1 || diffs[0].Package != "ros-humble-turtlebot3-msgs" {
		t.Fatalf("diffs = %+v", diffs)
	}
}
