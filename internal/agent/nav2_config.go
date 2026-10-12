package agent

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// nav2ParamGlobs find the TurtleBot3 nav2 params files that
// navigation2.launch.py loads (param/humble/<model>.yaml on Humble).
var nav2ParamGlobs = []string{
	"/opt/ros/*/share/turtlebot3_navigation2/param/*.yaml",
	"/opt/ros/*/share/turtlebot3_navigation2/param/*/*.yaml",
}

// nav2Override is one parameter set on top of the stock file.
type nav2Override struct {
	path  []string
	value string
}

func ros(node string, keys ...string) []string {
	path := append(strings.Split(node, "."), "ros__parameters")
	return append(path, keys...)
}

// nav2Overrides make SLAM navigation responsive out of the box. Entries for
// slam_toolbox only take effect because nav2's slam_launch.py passes this
// file to slam_toolbox when it has a slam_toolbox section -- in place of
// slam_toolbox's own config, which is why that config is copied in first
// (see slamDefaultsFile).
var nav2Overrides = []nav2Override{
	// The stock global costmap is sized to the SLAM map, which only covers
	// what has been explored: scan points past its edge are dropped ("sensor
	// origin is out of map bounds") until SLAM grows the map. A rolling
	// window follows the robot instead; the static layer still draws the
	// SLAM map into it. 30 m covers a lab; goals further than 15 m from the
	// robot cannot be planned to.
	{ros("global_costmap.global_costmap", "rolling_window"), "true"},
	{ros("global_costmap.global_costmap", "width"), "30"},
	{ros("global_costmap.global_costmap", "height"), "30"},
	{ros("global_costmap.global_costmap", "update_frequency"), "5.0"},
	{ros("global_costmap.global_costmap", "publish_frequency"), "2.0"},
	// slam_toolbox's defaults only take a scan after 0.5 m or 0.5 rad of
	// travel and publish the map every 5 s.
	{ros("slam_toolbox", "minimum_travel_distance"), "0.2"},
	{ros("slam_toolbox", "minimum_travel_heading"), "0.2"},
	{ros("slam_toolbox", "map_update_interval"), "1.0"},
	{ros("slam_toolbox", "transform_timeout"), "0.5"},
}

// slamDefaultsFile is slam_toolbox's stock config for the mode nav2's
// slam_launch.py starts, for the distro that params file belongs to.
func slamDefaultsFile(paramsFile string) string {
	parts := strings.SplitN(paramsFile, "/share/", 2)
	return parts[0] + "/share/slam_toolbox/config/mapper_params_online_sync.yaml"
}

const nav2ConfigHeader = "# Managed by openrobotfleet-agent: the package's file is diverted to\n" +
	"# %s.distrib and this copy is rebuilt from it with the fleet's overrides\n" +
	"# (internal/agent/nav2_config.go). Local edits are overwritten.\n"

// EnsureNav2Config applies nav2Overrides to every TurtleBot3 nav2 params
// file on the device. Each file is diverted with dpkg-divert, so a package
// upgrade writes the new stock file to <file>.distrib instead of replacing
// ours, and ours is rebuilt from that stock copy: upstream changes to other
// parameters still come through. Runs at agent start and after system
// updates; a no-op where TurtleBot3 navigation isn't installed.
func EnsureNav2Config() error {
	var files []string
	for _, pattern := range nav2ParamGlobs {
		matches, _ := filepath.Glob(pattern)
		files = append(files, matches...)
	}
	var errs []string
	for _, file := range files {
		if strings.HasSuffix(file, ".distrib") {
			continue
		}
		if err := ensureNav2File(file); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", file, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("nav2 config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func ensureNav2File(file string) error {
	stock := file + ".distrib"
	if _, err := os.Stat(stock); os.IsNotExist(err) {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !bytes.Contains(data, []byte("global_costmap:")) {
			return nil // not a nav2 params file
		}
		if out, err := runCmd(defaultCmdTimeout, "dpkg-divert", "--local", "--rename", "--divert", stock, "--add", file); err != nil {
			return fmt.Errorf("dpkg-divert: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	data, err := os.ReadFile(stock)
	if err != nil {
		return err
	}
	// Without slam_toolbox's stock config to build on, leave SLAM alone:
	// a partial slam_toolbox section would replace all of its settings.
	slamDefaults, _ := os.ReadFile(slamDefaultsFile(file))
	merged, err := applyNav2Overrides(data, slamDefaults)
	if err != nil {
		return err
	}
	merged = append([]byte(fmt.Sprintf(nav2ConfigHeader, file)), merged...)
	if existing, _ := os.ReadFile(file); bytes.Equal(existing, merged) {
		return nil
	}
	if err := os.WriteFile(file, merged, 0o644); err != nil {
		return err
	}
	log.Printf("[agent] applied fleet nav2 overrides to %s", file)
	return nil
}

// applyNav2Overrides sets each override in a params document, creating
// missing sections, and keeps everything else, comments included.
// slamDefaults is slam_toolbox's stock config; when it is nil, the
// slam_toolbox overrides are skipped.
func applyNav2Overrides(data, slamDefaults []byte) ([]byte, error) {
	root, doc, err := yamlRoot(data)
	if err != nil {
		return nil, err
	}
	slamParams := ros("slam_toolbox")
	if slamDefaults != nil {
		slamRoot, _, err := yamlRoot(slamDefaults)
		if err != nil {
			return nil, fmt.Errorf("slam_toolbox config: %w", err)
		}
		stock := findYAMLPath(slamRoot, slamParams)
		if stock == nil || stock.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("slam_toolbox config has no slam_toolbox.ros__parameters")
		}
		for i := 0; i < len(stock.Content); i += 2 {
			key := append(append([]string{}, slamParams...), stock.Content[i].Value)
			if findYAMLPath(root, key) == nil {
				setYAMLNode(root, key, stock.Content[i+1])
			}
		}
	}
	for _, o := range nav2Overrides {
		if slamDefaults == nil && o.path[0] == "slam_toolbox" {
			continue
		}
		setYAMLPath(root, o.path, o.value)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func yamlRoot(data []byte) (*yaml.Node, *yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("params file is not a YAML mapping")
	}
	return doc.Content[0], &doc, nil
}

func findYAMLPath(m *yaml.Node, path []string) *yaml.Node {
	for i := 0; m.Kind == yaml.MappingNode && i < len(m.Content); i += 2 {
		if m.Content[i].Value == path[0] {
			if len(path) == 1 {
				return m.Content[i+1]
			}
			return findYAMLPath(m.Content[i+1], path[1:])
		}
	}
	return nil
}

func setYAMLPath(m *yaml.Node, path []string, value string) {
	old := findYAMLPath(m, path)
	n := &yaml.Node{Kind: yaml.ScalarNode, Value: value}
	if old != nil {
		n.LineComment = old.LineComment
	}
	setYAMLNode(m, path, n)
}

// setYAMLNode puts value at path, creating missing mappings on the way.
func setYAMLNode(m *yaml.Node, path []string, value *yaml.Node) {
	for i := 0; i < len(m.Content); i += 2 {
		if m.Content[i].Value != path[0] {
			continue
		}
		if len(path) == 1 {
			*m.Content[i+1] = *value
			return
		}
		if m.Content[i+1].Kind != yaml.MappingNode {
			*m.Content[i+1] = yaml.Node{Kind: yaml.MappingNode}
		}
		setYAMLNode(m.Content[i+1], path[1:], value)
		return
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Value: path[0]}
	if len(path) == 1 {
		m.Content = append(m.Content, key, value)
		return
	}
	child := &yaml.Node{Kind: yaml.MappingNode}
	m.Content = append(m.Content, key, child)
	setYAMLNode(child, path[1:], value)
}
