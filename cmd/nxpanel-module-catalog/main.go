package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/trust"
	"github.com/luoye663/nxpanel/internal/update"
)

type moduleDefinition struct {
	nginxmodule.Module
	Version         string `json:"version"`
	BuildFromSource bool   `json:"build_from_source,omitempty"`
}

type releaseCandidate struct {
	Modules []nginxmodule.Module     `json:"modules"`
	Assets  []nginxmodule.Asset      `json:"assets"`
	Build   *nginxmodule.BuildRecipe `json:"build,omitempty"`
}

type buildTarget struct {
	nginxmodule.Target
	Compatibility []nginxmodule.Compatibility `json:"compatibility,omitempty"`
}

func main() {
	input := flag.String("input", "module-builds", "input directory")
	output := flag.String("output", "module-release", "output directory")
	sequence := flag.Uint64("sequence", 0, "local fixture index sequence")
	policyDir := flag.String("trust-dir", trust.Path, "directory with the signed root policy")
	metadata := flag.String("metadata", "", "path to module-definitions.json")
	prepare := flag.Bool("prepare", false, "prepare a source candidate or package local test builds")
	candidate := flag.Bool("candidate", false, "sign a source-only release candidate (read any candidate for -test-fixture)")
	fixture := flag.Bool("test-fixture", false, "generate a local index, GitHub-style releases, and ephemeral public trust key")
	flag.Parse()
	switch {
	case *prepare && !*candidate && !*fixture && *metadata != "" && *sequence == 0:
		definitions, err := loadDefinitions(*metadata)
		must(err)
		if len(definitions) != 1 {
			log.Fatal("a candidate must package exactly one module definition")
		}
		manifest, files, err := assembleManifest(*input, definitions[0])
		if err == nil {
			err = addJSON(files, "candidate.json", releaseCandidate{Modules: []nginxmodule.Module{manifest.Module}, Assets: manifest.Assets, Build: manifest.Build})
		}
		must(err)
		must(writeFiles(*output, files))
	case *candidate && !*prepare && !*fixture && *metadata == "" && *sequence == 0:
		must(signRelease(*input, *output, *policyDir))
	case *fixture && !*prepare && (*candidate == (*metadata == "")) && *sequence != 0:
		must(writeTestFixture(*input, *output, *metadata, *sequence))
	default:
		log.Fatal("choose -prepare, -candidate, or -test-fixture (with -metadata or -candidate)")
	}
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func loadDefinitions(path string) ([]moduleDefinition, error) {
	var definitions []moduleDefinition
	if err := readJSON(path, nginxmodule.MaxEnvelope, &definitions); err != nil {
		return nil, err
	}
	if len(definitions) == 0 || len(definitions) > nginxmodule.MaxModules {
		return nil, fmt.Errorf("invalid module definition count")
	}
	return definitions, nil
}

func signingKeys(public string, env string) (map[string]ed25519.PrivateKey, error) {
	keys := make(map[string]ed25519.PrivateKey)
	for _, entry := range strings.Split(os.Getenv(env), ",") {
		id, key, err := trust.AuthorizedPrivate(entry, public)
		if err != nil || keys[id] != nil {
			return nil, fmt.Errorf("invalid signing secret in %s", env)
		}
		keys[id] = key
	}
	return keys, nil
}

func signedIndex(raw []byte, sequence uint64, keys map[string]ed25519.PrivateKey, trust, updateKeys map[string]ed25519.PublicKey, test bool) (map[string][]byte, error) {
	signature := nginxmodule.IndexSignature{Sequence: sequence, Signatures: nginxmodule.Sign(nginxmodule.IndexPayload(raw, sequence), keys, nginxmodule.IndexPurpose, test)}
	files := map[string][]byte{nginxmodule.IndexName: raw}
	if err := addJSON(files, nginxmodule.IndexSigName, signature); err != nil {
		return nil, err
	}
	_, _, err := nginxmodule.VerifyIndex(raw, files[nginxmodule.IndexSigName], trust, updateKeys, nginxmodule.Floor{}, test)
	return files, err
}

func signRelease(input, output, policyDir string) error {
	manifest, err := loadCandidateManifest(input)
	if err != nil {
		return err
	}
	if err := manifest.RequireSourceOnly(); err != nil {
		return err
	}
	files, err := readCandidateFiles(input, manifest)
	if err != nil {
		return err
	}
	policy, err := trust.CurrentDir(policyDir, os.Getenv("NXPANEL_ROOT_PUBLIC_KEY"), time.Now())
	if err != nil {
		return err
	}
	module, ok := policy.Module(manifest.Module.ID)
	if !ok || module.Symbol != manifest.Module.Symbol || !strings.EqualFold(module.Repository, manifest.Module.Release.Repository) {
		return fmt.Errorf("module, repository or symbol not authorized by root policy")
	}
	entry := nginxmodule.IndexEntry(module)
	keys, err := signingKeys(entry.Keys, "NXPANEL_MODULE_RELEASE_SIGNING_KEYS")
	if err != nil {
		return err
	}
	if err := addManifest(files, manifest, entry, keys, false); err != nil {
		return err
	}
	inputDir, err := filepath.Abs(input)
	if err != nil {
		return err
	}
	outputDir, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	if inputDir == outputDir {
		files = map[string][]byte{nginxmodule.ManifestName: files[nginxmodule.ManifestName], nginxmodule.ManifestSig: files[nginxmodule.ManifestSig]}
	}
	return writeFiles(output, files)
}

func indexEntry(manifest nginxmodule.Manifest, keys string) nginxmodule.IndexEntry {
	module := manifest.Module
	return nginxmodule.IndexEntry{ID: module.ID, Name: module.Name, Description: module.Description, Symbol: module.Symbol, Repository: module.Release.Repository, Keys: keys}
}

func addManifest(files map[string][]byte, manifest nginxmodule.Manifest, entry nginxmodule.IndexEntry, keys map[string]ed25519.PrivateKey, test bool) error {
	if err := addJSON(files, nginxmodule.ManifestName, manifest); err != nil {
		return err
	}
	raw := files[nginxmodule.ManifestName]
	if err := addJSON(files, nginxmodule.ManifestSig, nginxmodule.Sign(raw, keys, nginxmodule.ReleasePurpose, test)); err != nil {
		return err
	}
	_, err := nginxmodule.VerifyManifest(raw, files[nginxmodule.ManifestSig], entry, manifest.Module.Release.Tag, test)
	return err
}

func assembleManifest(input string, definition moduleDefinition) (nginxmodule.Manifest, map[string][]byte, error) {
	manifest := nginxmodule.Manifest{Protocol: nginxmodule.Protocol, Module: definition.Module, Assets: []nginxmodule.Asset{}}
	module := &manifest.Module
	if !nginxmodule.ValidID(module.ID) || !nginxmodule.ValidSymbol(module.Symbol) {
		return manifest, nil, fmt.Errorf("invalid module ID or symbol")
	}
	files := make(map[string][]byte)
	dir := filepath.Join(input, module.ID)
	for _, action := range []struct {
		name string
		spec *nginxmodule.Script
	}{{"install", &module.Install}, {"uninstall", &module.Uninstall}} {
		body, err := readBuildFile(filepath.Join(dir, action.name+".sh"), nginxmodule.MaxScript)
		if err != nil {
			return manifest, nil, err
		}
		if len(body) == 0 || bytes.ContainsRune(body, 0) {
			return manifest, nil, fmt.Errorf("invalid module shell script")
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(body))
		name := nginxmodule.ScriptName(module.ID, action.name, digest)
		files[name] = body
		*action.spec = nginxmodule.Script{Name: name, Size: int64(len(body)), SHA256: digest}
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*", "target.json"))
	if err != nil || len(paths) == 0 && !definition.BuildFromSource || len(paths) > 128 {
		return manifest, nil, fmt.Errorf("invalid number of build targets for %s", module.ID)
	}
	for _, path := range paths {
		var target buildTarget
		if err := readJSON(path, 8<<10, &target); err != nil {
			return manifest, nil, err
		}
		body, err := readBuildFile(filepath.Join(filepath.Dir(path), module.Symbol+".so"), nginxmodule.MaxAsset)
		if err != nil {
			return manifest, nil, err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(body))
		name := nginxmodule.AssetName(module.ID, digest)
		files[name] = body
		manifest.Assets = append(manifest.Assets, nginxmodule.Asset{Module: module.ID, Version: definition.Version, Target: target.Target, Compatibility: target.Compatibility, Name: name, SHA256: digest, Size: int64(len(body))})
	}
	manifest.Protocol = nginxmodule.ManifestProtocol(manifest.Assets)
	if definition.BuildFromSource {
		body, err := readBuildFile(filepath.Join(dir, "source.tar.gz"), nginxmodule.MaxSource)
		if err != nil {
			return manifest, nil, err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(body))
		name := nginxmodule.SourceName(module.ID, digest)
		files[name] = body
		manifest.Protocol = nginxmodule.BuildProtocol
		manifest.Build = &nginxmodule.BuildRecipe{Source: nginxmodule.Script{Name: name, Size: int64(len(body)), SHA256: digest}}
	}
	return manifest, files, manifest.Validate()
}

func loadCandidateManifest(input string) (nginxmodule.Manifest, error) {
	var candidate releaseCandidate
	if err := readJSON(filepath.Join(input, "candidate.json"), nginxmodule.MaxEnvelope, &candidate); err != nil {
		return nginxmodule.Manifest{}, err
	}
	if len(candidate.Modules) != 1 {
		return nginxmodule.Manifest{}, fmt.Errorf("a release candidate must contain exactly one module")
	}
	manifest := nginxmodule.Manifest{Protocol: nginxmodule.ManifestProtocol(candidate.Assets), Module: candidate.Modules[0], Assets: candidate.Assets, Build: candidate.Build}
	if candidate.Build != nil {
		manifest.Protocol = nginxmodule.BuildProtocol
	}
	return manifest, manifest.Validate()
}

func readCandidate(input string) (nginxmodule.Manifest, map[string][]byte, error) {
	manifest, err := loadCandidateManifest(input)
	if err != nil {
		return manifest, nil, err
	}
	files, err := readCandidateFiles(input, manifest)
	return manifest, files, err
}

func readCandidateFiles(input string, manifest nginxmodule.Manifest) (map[string][]byte, error) {
	files := make(map[string][]byte)
	load := func(name string, size int64, digest string) error {
		body, err := readBuildFile(filepath.Join(input, name), size)
		if err != nil {
			return err
		}
		if int64(len(body)) != size || fmt.Sprintf("%x", sha256.Sum256(body)) != digest {
			return fmt.Errorf("candidate asset hash mismatch: %s", name)
		}
		files[name] = body
		return nil
	}
	for _, script := range []nginxmodule.Script{manifest.Module.Install, manifest.Module.Uninstall} {
		if err := load(script.Name, script.Size, script.SHA256); err != nil {
			return nil, err
		}
		if bytes.ContainsRune(files[script.Name], 0) {
			return nil, fmt.Errorf("invalid module shell script")
		}
	}
	for _, asset := range manifest.Assets {
		if err := load(asset.Name, asset.Size, asset.SHA256); err != nil {
			return nil, err
		}
	}
	if manifest.Build != nil {
		source := manifest.Build.Source
		if err := load(source.Name, source.Size, source.SHA256); err != nil {
			return nil, err
		}
	}
	return files, nil
}

func writeTestFixture(input, output, metadata string, sequence uint64) error {
	rootPublic, rootPrivate, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	feed := filepath.Join(output, "feed")
	var entries []nginxmodule.IndexEntry
	add := func(manifest nginxmodule.Manifest, files map[string][]byte, includeHistory bool) error {
		public, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			return err
		}
		const id = "module-release-fixture"
		entry := indexEntry(manifest, id+":"+base64.StdEncoding.EncodeToString(public))
		entries = append(entries, entry)
		root := filepath.Join(feed, "repos", entry.Repository, "releases")
		metadata := make(map[string][]byte)
		writeRelease := func(value nginxmodule.Manifest) (map[string]any, error) {
			tag := value.Module.Release.Tag
			if err := addManifest(files, value, entry, map[string]ed25519.PrivateKey{id: private}, true); err != nil {
				return nil, err
			}
			if err := writeFiles(filepath.Join(root, "download", tag), files); err != nil {
				return nil, err
			}
			info := map[string]any{"tag_name": tag, "draft": false, "prerelease": false, "published_at": "2020-01-01T00:00:00Z"}
			return info, addJSON(metadata, "tags/"+tag+".json", info)
		}
		latest, err := writeRelease(manifest)
		if err != nil {
			return err
		}
		releases := []any{latest}
		if includeHistory && manifest.Module.Release.Tag != "v0.0.0" {
			historical := manifest
			historical.Module.Release.Tag = "v0.0.0"
			historical.Assets = append([]nginxmodule.Asset(nil), manifest.Assets...)
			for index := range historical.Assets {
				historical.Assets[index].Version = historical.Module.Release.Tag
			}
			previous, err := writeRelease(historical)
			if err != nil {
				return err
			}
			releases = append(releases, previous)
		}
		if err := addJSON(metadata, "latest.json", latest); err != nil {
			return err
		}
		if err := addJSON(metadata, "pages/1.json", releases); err != nil {
			return err
		}
		return writeFiles(root, metadata)
	}
	if metadata == "" {
		manifest, files, err := readCandidate(input)
		if err != nil {
			return err
		}
		if err := add(manifest, files, false); err != nil {
			return err
		}
	} else {
		definitions, err := loadDefinitions(metadata)
		if err != nil {
			return err
		}
		for _, definition := range definitions {
			definition.Release = nginxmodule.Release{Repository: "fixture/" + definition.ID, Tag: definition.Version}
			manifest, files, err := assembleManifest(input, definition)
			if err != nil {
				return err
			}
			if err := add(manifest, files, true); err != nil {
				return err
			}
		}
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	const id = "module-index-fixture"
	files, err := signedIndex(raw, sequence, map[string]ed25519.PrivateKey{id: rootPrivate}, map[string]ed25519.PublicKey{id: rootPublic}, nil, true)
	if err != nil {
		return err
	}
	if err := writeFiles(feed, files); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "trust.pub"), []byte(id+":"+base64.StdEncoding.EncodeToString(rootPublic)+"\n"), 0644)
}

func addJSON(files map[string][]byte, name string, value any) error {
	body, err := json.Marshal(value)
	if err == nil {
		files[name] = append(body, '\n')
	}
	return err
}

func writeFiles(output string, files map[string][]byte) error {
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	for name, body := range files {
		path := filepath.Join(output, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(path, body, 0644); err != nil {
			return err
		}
	}
	return nil
}

func readJSON(path string, limit int64, target any) error {
	body, err := readBuildFile(path, limit)
	if err != nil {
		return err
	}
	return update.Decode(bytes.NewReader(body), target)
}

func readBuildFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) > limit {
		err = fmt.Errorf("file exceeds size limit: %s", path)
	}
	return data, err
}
