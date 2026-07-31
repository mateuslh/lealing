// Package marketplace contém o modelo neutro do índice de distribuição. O
// core não conhece GitHub nem HTTP: qualquer origem futura implementará uma
// porta em volta destes valores.
package marketplace

import (
	"errors"
	"sort"
	"strconv"
	"strings"
)

type Channel string

const (
	ChannelOfficial  Channel = "official"
	ChannelVerified  Channel = "verified"
	ChannelCommunity Channel = "community"
)

type VersionRange struct{ Min, Max int }

type Artifact struct {
	Platform string `json:"platform" yaml:"platform"`
	URL      string `json:"url" yaml:"url"`
	SHA256   string `json:"sha256" yaml:"sha256"`
}

type Entry struct {
	ID               string       `json:"id" yaml:"id"`
	Version          string       `json:"version" yaml:"version"`
	Publisher        string       `json:"publisher" yaml:"publisher"`
	ManifestURL      string       `json:"manifestUrl" yaml:"manifestUrl"`
	Artifacts        []Artifact   `json:"artifacts" yaml:"artifacts"`
	Protocol         VersionRange `json:"protocol" yaml:"protocol"`
	MinimumEngine    string       `json:"minimumEngine" yaml:"minimumEngine"`
	DistributionTier Channel      `json:"channel" yaml:"channel"`
}

type Index struct {
	APIVersion string  `json:"apiVersion" yaml:"apiVersion"`
	Tools      []Entry `json:"tools" yaml:"tools"`
}

// SelectLatest escolhe a versão mais nova compatível sem fixar a engine a
// uma versão específica da tool.
func (i Index) SelectLatest(id, platform, engineVersion string, protocol VersionRange) (Entry, Artifact, error) {
	type candidate struct {
		entry    Entry
		artifact Artifact
		version  [3]int
	}
	var candidates []candidate
	for _, entry := range i.Tools {
		if entry.ID != id || entry.Protocol.Max < protocol.Min || protocol.Max < entry.Protocol.Min {
			continue
		}
		version, ok := parseVersion(entry.Version)
		minimum, minOK := parseVersion(entry.MinimumEngine)
		engine, engineOK := parseVersion(engineVersion)
		if !ok || (entry.MinimumEngine != "" && (!minOK || !engineOK || less(engine, minimum))) {
			continue
		}
		for _, artifact := range entry.Artifacts {
			if artifact.Platform == platform && artifact.URL != "" && len(artifact.SHA256) == 64 {
				candidates = append(candidates, candidate{entry: entry, artifact: artifact, version: version})
			}
		}
	}
	if len(candidates) == 0 {
		return Entry{}, Artifact{}, errors.New("nenhuma versão compatível no marketplace")
	}
	sort.Slice(candidates, func(a, b int) bool { return less(candidates[b].version, candidates[a].version) })
	return candidates[0].entry, candidates[0].artifact, nil
}

func parseVersion(value string) ([3]int, bool) {
	value = strings.TrimPrefix(value, "v")
	core, _, _ := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var result [3]int
	for index, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		result[index] = n
	}
	return result, true
}

func less(a, b [3]int) bool {
	for index := range a {
		if a[index] != b[index] {
			return a[index] < b[index]
		}
	}
	return false
}
