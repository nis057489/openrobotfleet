package agent

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNav2OverridesOnStockWafflePi(t *testing.T) {
	stock, err := os.ReadFile("testdata/waffle_pi_humble.yaml")
	if err != nil {
		t.Fatal(err)
	}
	slam, err := os.ReadFile("testdata/mapper_params_online_sync_humble.yaml")
	if err != nil {
		t.Fatal(err)
	}
	out, err := applyNav2Overrides(stock, slam)
	if err != nil {
		t.Fatal(err)
	}
	var got, orig map[string]any
	if err := yaml.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}
	yaml.Unmarshal(stock, &orig)

	get := func(m map[string]any, path []string) any {
		var cur any = m
		for _, k := range path {
			mm, ok := cur.(map[string]any)
			if !ok {
				return nil
			}
			cur = mm[k]
		}
		return cur
	}
	for _, o := range nav2Overrides {
		if v := get(got, o.path); v == nil || strings.TrimSuffix(strings.TrimSuffix(fmtAny(v), ".0"), ".0") != strings.TrimSuffix(o.value, ".0") {
			t.Errorf("%s = %v, want %s", strings.Join(o.path, "."), v, o.value)
		}
	}
	// Everything else is untouched.
	for _, path := range [][]string{
		ros("global_costmap.global_costmap", "plugins"),
		ros("global_costmap.global_costmap", "robot_radius"),
		ros("local_costmap.local_costmap", "rolling_window"),
		ros("local_costmap.local_costmap", "width"),
		ros("amcl", "max_particles"),
		ros("planner_server", "GridBased", "allow_unknown"),
	} {
		if a, b := fmtAny(get(orig, path)), fmtAny(get(got, path)); a != b || a == "<nil>" {
			t.Errorf("%s changed: %v -> %v", strings.Join(path, "."), a, b)
		}
	}
	// slam_toolbox gets its whole stock config, not just the overrides,
	// since nav2 passes this file in place of slam_toolbox's own.
	var slamStock map[string]any
	yaml.Unmarshal(slam, &slamStock)
	stockParams := get(slamStock, ros("slam_toolbox")).(map[string]any)
	gotParams := get(got, ros("slam_toolbox")).(map[string]any)
	if len(gotParams) != len(stockParams) {
		t.Errorf("slam_toolbox has %d params, stock has %d", len(gotParams), len(stockParams))
	}
	for _, k := range []string{"solver_plugin", "base_frame", "mode", "do_loop_closing", "max_laser_range"} {
		if fmtAny(gotParams[k]) != fmtAny(stockParams[k]) {
			t.Errorf("slam_toolbox.%s = %v, stock %v", k, gotParams[k], stockParams[k])
		}
	}
	// Applying twice gives the same file (the agent rewrites only on change).
	again, _ := applyNav2Overrides(stock, slam)
	if string(again) != string(out) {
		t.Error("output is not deterministic")
	}
}

func fmtAny(v any) string { b, _ := yaml.Marshal(v); return strings.TrimSpace(string(b)) }

func TestNav2OverridesSkipSLAMWithoutItsStockConfig(t *testing.T) {
	stock, _ := os.ReadFile("testdata/waffle_pi_humble.yaml")
	out, err := applyNav2Overrides(stock, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	yaml.Unmarshal(out, &got)
	if _, ok := got["slam_toolbox"]; ok {
		t.Fatal("added a partial slam_toolbox section, which would replace all of its settings")
	}
	if fmtAny(got["global_costmap"].(map[string]any)["global_costmap"].(map[string]any)["ros__parameters"].(map[string]any)["rolling_window"]) != "true" {
		t.Fatal("costmap overrides missing")
	}
}

func TestSlamDefaultsFile(t *testing.T) {
	got := slamDefaultsFile("/opt/ros/humble/share/turtlebot3_navigation2/param/humble/waffle_pi.yaml")
	if got != "/opt/ros/humble/share/slam_toolbox/config/mapper_params_online_sync.yaml" {
		t.Fatal(got)
	}
}
