package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type CameraConfig struct {
	VFlip  bool
	HFlip  bool
	Width  int
	Height int
	AWB    string
	Mode   string
}

func LoadCameraConfig(path string) (CameraConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return CameraConfig{}, err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return CameraConfig{}, err
	}

	pathNode, err := findPathNode(&root, "cam")
	if err != nil {
		return CameraConfig{}, err
	}

	config := CameraConfig{}
	if v, ok, err := getBool(pathNode, "rpiCameraVFlip"); err != nil {
		return CameraConfig{}, err
	} else if ok {
		config.VFlip = v
	}
	if v, ok, err := getBool(pathNode, "rpiCameraHFlip"); err != nil {
		return CameraConfig{}, err
	} else if ok {
		config.HFlip = v
	}
	if v, ok, err := getInt(pathNode, "rpiCameraWidth"); err != nil {
		return CameraConfig{}, err
	} else if ok {
		config.Width = v
	}
	if v, ok, err := getInt(pathNode, "rpiCameraHeight"); err != nil {
		return CameraConfig{}, err
	} else if ok {
		config.Height = v
	}
	if v, ok, err := getString(pathNode, "rpiCameraAWB"); err != nil {
		return CameraConfig{}, err
	} else if ok {
		config.AWB = v
	}
	if v, ok, err := getString(pathNode, "rpiCameraMode"); err != nil {
		return CameraConfig{}, err
	} else if ok {
		config.Mode = v
	}

	return config, nil
}

func SaveCameraConfig(path string, config CameraConfig) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(b, &root); err != nil {
		return err
	}

	pathNode, err := findPathNode(&root, "cam")
	if err != nil {
		return err
	}

	setBool(pathNode, "rpiCameraVFlip", config.VFlip)
	setBool(pathNode, "rpiCameraHFlip", config.HFlip)
	if config.Width > 0 {
		setInt(pathNode, "rpiCameraWidth", config.Width)
	}
	if config.Height > 0 {
		setInt(pathNode, "rpiCameraHeight", config.Height)
	}
	if config.AWB != "" {
		setString(pathNode, "rpiCameraAWB", config.AWB)
	}
	if config.Mode != "" {
		setString(pathNode, "rpiCameraMode", config.Mode)
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}

	var check yaml.Node
	if err := yaml.Unmarshal(out, &check); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "mediamtx.yml.tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode()); err != nil {
		return err
	}

	backup := path + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.Rename(path, backup); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Rename(backup, path)
		return err
	}

	return nil
}

func findPathNode(root *yaml.Node, name string) (*yaml.Node, error) {
	mapping := rootMapping(root)
	if mapping == nil {
		return nil, errors.New("invalid yaml root")
	}

	pathsNode := findMapValue(mapping, "paths")
	if pathsNode == nil || pathsNode.Kind != yaml.MappingNode {
		return nil, errors.New("paths section not found")
	}

	pathNode := findMapValue(pathsNode, name)
	if pathNode == nil || pathNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("path %q not found", name)
	}
	return pathNode, nil
}

func rootMapping(root *yaml.Node) *yaml.Node {
	node := root
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

func findMapValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Value == key {
			return v
		}
	}
	return nil
}

func setBool(mapping *yaml.Node, key string, value bool) {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Value == key {
			v.Kind = yaml.ScalarNode
			v.Tag = "!!bool"
			v.Value = strconv.FormatBool(value)
			return
		}
	}

	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)},
	)
}

func getBool(mapping *yaml.Node, key string) (bool, bool, error) {
	node := findMapValue(mapping, key)
	if node == nil {
		return false, false, nil
	}
	value := strings.TrimSpace(node.Value)
	if value == "" {
		return false, false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, true, fmt.Errorf("invalid bool for %s", key)
	}
	return parsed, true, nil
}

func setInt(mapping *yaml.Node, key string, value int) {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Value == key {
			v.Kind = yaml.ScalarNode
			v.Tag = "!!int"
			v.Value = strconv.Itoa(value)
			return
		}
	}

	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(value)},
	)
}

func getInt(mapping *yaml.Node, key string) (int, bool, error) {
	node := findMapValue(mapping, key)
	if node == nil {
		return 0, false, nil
	}
	value := strings.TrimSpace(node.Value)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, true, fmt.Errorf("invalid int for %s", key)
	}
	return parsed, true, nil
}

func setString(mapping *yaml.Node, key, value string) {
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		k := mapping.Content[i]
		v := mapping.Content[i+1]
		if k.Value == key {
			v.Kind = yaml.ScalarNode
			v.Tag = "!!str"
			v.Value = value
			return
		}
	}

	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func getString(mapping *yaml.Node, key string) (string, bool, error) {
	node := findMapValue(mapping, key)
	if node == nil {
		return "", false, nil
	}
	value := strings.TrimSpace(node.Value)
	if value == "" {
		return "", false, nil
	}
	return value, true, nil
}
