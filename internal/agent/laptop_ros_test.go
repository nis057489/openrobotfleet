package agent

import (
	"strings"
	"testing"
)

func TestIsROSProcess(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/usr/bin/python3 /opt/ros/humble/bin/ros2 launch turtlebot3_navigation2 navigation2.launch.py": true,
		"/usr/bin/python3 /opt/ros/humble/bin/_ros2_daemon --rmw-implementation rmw_cyclonedds_cpp":     true,
		"/opt/ros/humble/lib/nav2_planner/planner_server --ros-args -r __node:=planner_server":          true,
		"/opt/ros/humble/lib/slam_toolbox/sync_slam_toolbox_node --ros-args":                            true,
		"/opt/ros/humble/lib/rviz2/rviz2 -d nav2.rviz":                                                  true,
		"/usr/bin/python3 -u /home/m/metr4202_ws/install/explorer_pkg/lib/explorer_pkg/explorer_node":   true,
		"/home/m/metr4202_ws/install/my_cpp/lib/my_cpp/node":                                            true,
		"gzserver -s libgazebo_ros_init.so world.world":                                                 true,
		"gzclient":                       true,
		"/bin/bash":                      false,
		"/usr/bin/gnome-terminal-server": false,
		"/usr/share/code/code /opt/ros/humble/share/nav2/params.yaml": false,
		"less /opt/ros/humble/setup.bash":                             false,
		"/usr/bin/python3 /usr/local/bin/time_indicator.py":           false,
		"/usr/bin/python3 -c import rclpy":                            false,
	} {
		if got := isROSProcess(strings.Fields(cmd)); got != want {
			t.Errorf("isROSProcess(%q) = %v, want %v", cmd, got, want)
		}
	}
	if isROSProcess(nil) {
		t.Error("isROSProcess(nil) = true")
	}
}

func TestProcStatusField(t *testing.T) {
	status := []byte("Name:\tplanner_server\nState:\tS (sleeping)\nPPid:\t1\nUid:\t1000\t1000\t1000\t1000\n")
	for field, want := range map[string]string{"State": "S", "PPid": "1", "Uid": "1000", "Gid": ""} {
		if got := procStatusField(status, field); got != want {
			t.Errorf("procStatusField(%s) = %q, want %q", field, got, want)
		}
	}
}
