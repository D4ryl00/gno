package parse

import (
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/gnolang/gno/contribs/valdoctor/internal/model"
	"github.com/pelletier/go-toml"
)

func LoadMetadata(path string) (model.Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Metadata{}, err
	}

	var out model.Metadata
	if err := toml.Unmarshal(data, &out); err != nil {
		return model.Metadata{}, err
	}

	if out.Version == 0 {
		out.Version = 1
	}
	if out.Nodes == nil {
		out.Nodes = map[string]model.MetadataNode{}
	}
	if out.Topology.ValidatorToSentries == nil {
		out.Topology.ValidatorToSentries = map[string][]string{}
	}
	if out.PeerAliases == nil {
		out.PeerAliases = map[string]string{}
	}

	return out, nil
}

func MergeMetadata(items ...model.Metadata) model.Metadata {
	merged := model.Metadata{
		Version:     1,
		Nodes:       map[string]model.MetadataNode{},
		PeerAliases: map[string]string{},
		Topology: model.MetadataTopology{
			ValidatorToSentries: map[string][]string{},
		},
	}

	for _, item := range items {
		if item.ChainID != "" {
			merged.ChainID = item.ChainID
		}
		if item.Version != 0 {
			merged.Version = item.Version
		}
		for name, node := range item.Nodes {
			merged.Nodes[name] = mergeMetadataNode(merged.Nodes[name], node)
		}
		for key, value := range item.PeerAliases {
			merged.PeerAliases[key] = value
		}
		for key, value := range item.Topology.ValidatorToSentries {
			merged.Topology.ValidatorToSentries[key] = append([]string(nil), value...)
		}
	}

	return merged
}

func mergeMetadataNode(dst, src model.MetadataNode) model.MetadataNode {
	if src.Role != "" {
		dst.Role = src.Role
	}
	if len(src.Files) > 0 {
		for _, file := range src.Files {
			if !slices.Contains(dst.Files, file) {
				dst.Files = append(dst.Files, file)
			}
		}
	}
	if src.NodeID != "" {
		dst.NodeID = src.NodeID
	}
	if src.ValidatorName != "" {
		dst.ValidatorName = src.ValidatorName
	}
	if src.ValidatorAddress != "" {
		dst.ValidatorAddress = src.ValidatorAddress
	}
	if src.ValidatorPubKey != "" {
		dst.ValidatorPubKey = src.ValidatorPubKey
	}
	return dst
}

func WriteMetadata(path string, meta model.Metadata) error {
	data, err := toml.Marshal(meta)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func BuildGeneratedMetadata(genesis model.Genesis, sources []model.Source) model.Metadata {
	meta := model.Metadata{
		Version: 1,
		ChainID: genesis.ChainID,
		Nodes:   map[string]model.MetadataNode{},
		Topology: model.MetadataTopology{
			ValidatorToSentries: map[string][]string{},
		},
		PeerAliases: map[string]string{},
	}

	nodes := map[string]*model.MetadataNode{}
	order := make([]string, 0, len(sources))

	for _, source := range sources {
		node := source.Node
		if _, ok := nodes[node]; !ok {
			order = append(order, node)
			nodes[node] = &model.MetadataNode{
				Role: string(source.Role),
			}
		}
		nodes[node].Files = append(nodes[node].Files, source.Path)
		if nodes[node].Role == "" || nodes[node].Role == string(model.RoleUnknown) {
			nodes[node].Role = string(source.Role)
		}
	}

	sort.Strings(order)
	for _, name := range order {
		node := *nodes[name]
		sort.Strings(node.Files)
		meta.Nodes[name] = node
	}

	if genesis.ValidatorNum == 1 {
		validatorNodes := make([]string, 0)
		for name, node := range meta.Nodes {
			if node.Role == string(model.RoleValidator) {
				validatorNodes = append(validatorNodes, name)
			}
		}
		sort.Strings(validatorNodes)
		if len(validatorNodes) == 1 {
			name := validatorNodes[0]
			node := meta.Nodes[name]
			node.ValidatorName = genesis.Validators[0].Name
			node.ValidatorAddress = genesis.Validators[0].Address
			node.ValidatorPubKey = genesis.Validators[0].PubKey
			meta.Nodes[name] = node
		}
	}

	return meta
}

func NormalizePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}
